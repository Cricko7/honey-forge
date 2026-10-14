"""Хранилище цепочек: in-memory + JSON-persistence на диск.

Данные синтетические (fake). При первом запуске засевается демо-контентом и
встроенными шаблонами сценариев, чтобы доска не была пустой.
"""
from __future__ import annotations

import json
import threading
import time
from pathlib import Path
from typing import Any

from .models import (
    Chain, ChainCreate, ChainSummary, ChainUpdate, Edge, Node, Position,
)
from .node_registry import default_config

DATA_DIR = Path(__file__).resolve().parent.parent / "data"
DATA_FILE = DATA_DIR / "chains.json"


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
    def __init__(self) -> None:
        self._lock = threading.RLock()
        self._chains: dict[str, Chain] = {}
        self._load()
        if not self._chains:
            self._seed()

    # --- persistence ---
    def _load(self) -> None:
        if not DATA_FILE.exists():
            return
        try:
            raw = json.loads(DATA_FILE.read_text(encoding="utf-8"))
            for item in raw.get("chains", []):
                c = Chain.model_validate(item)
                self._chains[c.id] = c
        except Exception:
            pass

    def _save(self) -> None:
        DATA_DIR.mkdir(parents=True, exist_ok=True)
        payload = {"chains": [c.model_dump() for c in self._chains.values()]}
        DATA_FILE.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")

    def _seed(self) -> None:
        for tpl_id, tag in (("dmz", "demo"), ("internal", "demo")):
            tpl = TEMPLATES[tpl_id]
            nodes, edges = tpl["build"]()
            c = Chain(name=tpl["name"], description=tpl["description"], tags=[tag],
                      nodes=nodes, edges=edges)
            self._chains[c.id] = c
        self._save()

    # --- CRUD ---
    def list(self) -> list[ChainSummary]:
        with self._lock:
            return [ChainSummary(**c.model_dump(exclude={"nodes", "edges", "viewport"}),
                                 node_count=len(c.nodes), edge_count=len(c.edges))
                    for c in sorted(self._chains.values(), key=lambda x: x.updated_at, reverse=True)]

    def get(self, chain_id: str) -> Chain | None:
        with self._lock:
            return self._chains.get(chain_id)

    def create(self, data: ChainCreate) -> Chain:
        with self._lock:
            nodes: list[Node] = []
            edges: list[Edge] = []
            if data.template and data.template in TEMPLATES:
                nodes, edges = TEMPLATES[data.template]["build"]()
            c = Chain(name=data.name, description=data.description, tags=data.tags,
                      nodes=nodes, edges=edges)
            self._chains[c.id] = c
            self._save()
            return c

    def update(self, chain_id: str, data: ChainUpdate) -> Chain | None:
        with self._lock:
            c = self._chains.get(chain_id)
            if not c:
                return None
            patch = data.model_dump(exclude_none=True)
            for k, v in patch.items():
                setattr(c, k, v)
            c.updated_at = time.time()
            self._chains[chain_id] = c
            self._save()
            return c

    def delete(self, chain_id: str) -> bool:
        with self._lock:
            if chain_id in self._chains:
                del self._chains[chain_id]
                self._save()
                return True
            return False

    def duplicate(self, chain_id: str) -> Chain | None:
        with self._lock:
            src = self._chains.get(chain_id)
            if not src:
                return None
            clone = Chain.model_validate(src.model_dump())
            clone.id = Chain().id
            clone.name = f"{src.name} (копия)"
            clone.created_at = clone.updated_at = time.time()
            self._chains[clone.id] = clone
            self._save()
            return clone


store = ChainStore()
