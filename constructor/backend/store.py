"""Доски организаций: память и сохранение в JSON."""
from __future__ import annotations

import json
import os
from contextvars import ContextVar
import threading
import time
from pathlib import Path
from typing import Any

from .models import (
    Chain, ChainCreate, ChainSummary, ChainUpdate, Edge, Node, Position,
)
from .node_registry import default_config
from .request_json import check_values

DATA_DIR = Path(os.environ.get('CONSTRUCTOR_DATA_DIR', Path(__file__).resolve().parent.parent / 'data'))
organization = ContextVar('organization')


def _node(ntype: str, x: float, y: float, label: str | None = None,
          cfg: dict[str, Any] | None = None) -> Node:
    config = default_config(ntype)
    if cfg:
        config.update(cfg)
    return Node(type=ntype, position=Position(x=x, y=y), label=label, config=config)


def _tpl_dmz() -> tuple[list[Node], list[Edge]]:
    n_net = _node("entry/internet", 60, 260, "Интернет")
    n_web = _node("trap/http-app", 360, 120, "DMZ-портал")
    n_ssh = _node("trap/ssh", 360, 400, "SSH-шлюз DMZ")
    n_tok = _node("lure/honeytoken", 680, 400, "Ключ svc-backup",
                  {"kind": "ssh-keypair", "username": "svc-backup"})
    n_ws = _node("endpoint/workstation", 1000, 260, "АРМ бухгалтера")
    n_alert = _node("output/alert", 1320, 120, "Алерт SOC", {"severity": "critical"})
    n_siem = _node("output/siem", 1320, 400, "SIEM")
    nodes = [n_net, n_web, n_ssh, n_tok, n_ws, n_alert, n_siem]
    edges = [
        Edge(source=n_net.id, source_port="out", target=n_web.id, target_port="in"),
        Edge(source=n_net.id, source_port="out", target=n_ssh.id, target_port="in"),
        Edge(source=n_ssh.id, source_port="loot", target=n_tok.id, target_port="in", label="нашёл ключ"),
        Edge(source=n_tok.id, source_port="out", target=n_ws.id, target_port="creds_in", label="креды подошли"),
        Edge(source=n_ws.id, source_port="out", target=n_alert.id, target_port="in"),
        Edge(source=n_web.id, source_port="out", target=n_siem.id, target_port="in"),
    ]
    return nodes, edges


def _tpl_internal() -> tuple[list[Node], list[Edge]]:
    n_lan = _node("entry/lan", 60, 260, "Внутренняя сеть")
    n_smb = _node("trap/smb", 360, 160, "Файловый сервер")
    n_doc = _node("lure/document", 680, 160, "passwords_final.xlsx")
    n_cond = _node("logic/condition", 680, 380, "Нашёл креды?")
    n_dc = _node("endpoint/dc", 1020, 260, "Контроллер домена")
    n_alert = _node("output/alert", 1320, 260, "Алерт SOC", {"severity": "critical"})
    nodes = [n_lan, n_smb, n_doc, n_cond, n_dc, n_alert]
    edges = [
        Edge(source=n_lan.id, source_port="out", target=n_smb.id, target_port="in"),
        Edge(source=n_smb.id, source_port="loot", target=n_doc.id, target_port="in"),
        Edge(source=n_smb.id, source_port="out", target=n_cond.id, target_port="in"),
        Edge(source=n_cond.id, source_port="out_true", target=n_dc.id, target_port="creds_in", label="да"),
        Edge(source=n_dc.id, source_port="out", target=n_alert.id, target_port="in"),
    ]
    return nodes, edges


def _tpl_endpoint() -> tuple[list[Node], list[Edge]]:
    n_lan = _node("entry/lan", 60, 220, "Фишинг → АРМ")
    n_ws = _node("endpoint/workstation", 360, 220, "АРМ i.petrov")
    n_tok = _node("lure/honeytoken", 680, 120, "Доменный cred")
    n_rdp = _node("trap/rdp", 1000, 220, "RDP сервер")
    n_siem = _node("output/siem", 1320, 220, "SIEM / ATT&CK")
    nodes = [n_lan, n_ws, n_tok, n_rdp, n_siem]
    edges = [
        Edge(source=n_lan.id, source_port="out", target=n_ws.id, target_port="in"),
        Edge(source=n_ws.id, source_port="loot", target=n_tok.id, target_port="in"),
        Edge(source=n_tok.id, source_port="out", target=n_rdp.id, target_port="creds_in", label="pass-the-hash"),
        Edge(source=n_rdp.id, source_port="out", target=n_siem.id, target_port="in"),
    ]
    return nodes, edges


TEMPLATES: dict[str, dict[str, Any]] = {
    "dmz": {"name": "DMZ-периметр", "description": "Внешний портал + SSH-шлюз, провал через honeytoken на АРМ.", "build": _tpl_dmz},
    "internal": {"name": "Внутренняя сеть", "description": "Файловый сервер → документ-приманка → контроллер домена.", "build": _tpl_internal},
    "endpoint": {"name": "Рабочая станция", "description": "Endpoint-deception: АРМ → кража кредов → RDP.", "build": _tpl_endpoint},
}


def templates_payload() -> list[dict[str, str]]:
    return [{"id": k, "name": v["name"], "description": v["description"]} for k, v in TEMPLATES.items()]


class ChainStore:
    def __init__(self, org: str) -> None:
        self.data_file = DATA_DIR / (org + '.json')
        self._lock = threading.RLock()
        self._chains: dict[str, Chain] = {}
        self._load()

    def _load(self) -> None:
        if not self.data_file.exists():
            return
        try:
            raw = json.loads(self.data_file.read_text(encoding="utf-8"))
            for item in raw.get("chains", []):
                c = Chain.model_validate(item)
                self._chains[c.id] = c
        except (ValueError, OSError) as error:
            raise RuntimeError('Cannot load constructor chains') from error

    def _commit(self, chains: dict[str, Chain]) -> None:
        DATA_DIR.mkdir(parents=True, exist_ok=True)
        payload = {"chains": [c.model_dump() for c in chains.values()]}
        check_values(payload)
        encoded = json.dumps(payload, ensure_ascii=False, allow_nan=False, indent=2)
        temporary = self.data_file.with_suffix('.tmp')
        temporary.write_text(encoded, encoding="utf-8")
        temporary.replace(self.data_file)
        self._chains = chains

    def _seed(self) -> None:
        c = Chain(name="Моя схема", description="", tags=[], nodes=[], edges=[])
        self._commit({**self._chains, c.id: c})

    def list(self) -> list[ChainSummary]:
        with self._lock:
            return [ChainSummary(**c.model_dump(exclude={"nodes", "edges", "viewport"}),
                                 node_count=len(c.nodes), edge_count=len(c.edges))
                    for c in sorted(self._chains.values(), key=lambda x: x.updated_at, reverse=True)]

    def get(self, chain_id: str) -> Chain | None:
        with self._lock:
            chain = self._chains.get(chain_id)
            return chain.model_copy(deep=True) if chain else None

    def save(self, chain: Chain) -> None:
        with self._lock:
            saved = chain.model_copy(deep=True)
            saved.updated_at = time.time()
            self._commit({**self._chains, saved.id: saved})

    def create(self, data: ChainCreate) -> Chain:
        with self._lock:
            nodes: list[Node] = []
            edges: list[Edge] = []
            if data.template and data.template in TEMPLATES:
                nodes, edges = TEMPLATES[data.template]["build"]()
            c = Chain(name=data.name, description=data.description, tags=data.tags,
                      nodes=nodes, edges=edges)
            self._commit({**self._chains, c.id: c})
            return c.model_copy(deep=True)

    def update(self, chain_id: str, data: ChainUpdate) -> Chain | None:
        with self._lock:
            c = self.get(chain_id)
            if not c:
                return None
            patch = {k: getattr(data, k) for k in data.model_fields_set if getattr(data, k) is not None}
            if data.nodes is not None:
                previous = {n.id: n for n in c.nodes}
                for node in data.nodes:
                    old = previous.get(node.id)
                    if old and 'password' not in node.config and 'password' in old.config:
                        node.config['password'] = old.config['password']
            for k, v in patch.items():
                setattr(c, k, v)
            c.updated_at = time.time()
            self._commit({**self._chains, chain_id: c})
            return c.model_copy(deep=True)

    def delete(self, chain_id: str) -> bool:
        with self._lock:
            if chain_id in self._chains:
                self._commit({key: chain for key, chain in self._chains.items() if key != chain_id})
                return True
            return False

    def duplicate(self, chain_id: str) -> Chain | None:
        with self._lock:
            src = self._chains.get(chain_id)
            if not src:
                return None
            clone = Chain.model_validate(src.model_dump())
            clone.id = Chain().id
            clone.bindings = {}
            for node in clone.nodes:
                node.config.pop('_real', None)
            clone.name = f"{src.name} (копия)"
            clone.created_at = clone.updated_at = time.time()
            self._commit({**self._chains, clone.id: clone})
            return clone.model_copy(deep=True)


class OrganizationStore:
    def __init__(self):
        self.stores = {}
        self.lock = threading.RLock()

    def __getattr__(self, name):
        with self.lock:
            org = organization.get()
            if org not in self.stores:
                self.stores[org] = ChainStore(org)
            return getattr(self.stores[org], name)


store = OrganizationStore()
