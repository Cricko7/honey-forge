"""Рантайм ловушек и живой движок атак (fake, но ведёт себя как боевой).

Отвечает за enterprise-слой конструктора:
- развёртывание цепочки (deploy) → узлы-ловушки переходят в состояние running;
- генерация живого потока атак: синтетический нарушитель «проваливается» по
  attack-path, на каждом шаге рождается событие (источник, страна, техника ATT&CK);
- накопление KPI (активные ловушки, события, уникальные источники, инциденты).

Поток отдаётся фронту по SSE (`/api/chains/{id}/stream`). Данные пока синтетические,
но формат событий спроектирован так, чтобы позже подключить боевой Go-бэкенд HoneyForge.
"""
from __future__ import annotations

import asyncio
import json
import random
import time
from typing import Any, AsyncIterator

from .models import Chain, SimulateRequest
from .simulator import simulate
from .store import store

COUNTRIES = [
    ("RU", "Россия"), ("CN", "Китай"), ("US", "США"), ("NL", "Нидерланды"),
    ("DE", "Германия"), ("BR", "Бразилия"), ("IN", "Индия"), ("IR", "Иран"),
    ("VN", "Вьетнам"), ("UA", "Украина"),
]
TOOLS = ["nmap", "hydra", "masscan", "metasploit", "sqlmap", "crackmapexec", "ручной доступ"]


def _rand_ip(rng: random.Random) -> str:
    return ".".join(str(rng.randint(1, 254)) for _ in range(4))


class RuntimeManager:
    """Хранит состояние развёртывания и KPI по каждой цепочке (in-memory)."""

    def __init__(self) -> None:
        self._state: dict[str, dict[str, Any]] = {}

    def _init(self, chain_id: str) -> dict[str, Any]:
        return self._state.setdefault(chain_id, {
            "deployed": False,
            "deployed_at": None,
            "node_status": {},        # node_id -> "running" | "stopped"
            "kpi": {"events_total": 0, "attacks": 0, "sources": set(), "login_attempts": 0},
        })

    def status(self, chain_id: str) -> dict[str, Any]:
        st = self._init(chain_id)
        chain = store.get(chain_id)
        trap_nodes = [n for n in (chain.nodes if chain else [])
                      if n.type.startswith(("trap/", "endpoint/"))]
        running = sum(1 for n in trap_nodes if st["node_status"].get(n.id) == "running")
        return {
            "deployed": st["deployed"],
            "deployed_at": st["deployed_at"],
            "node_status": st["node_status"],
            "kpi": self._kpi_public(chain_id, running, len(trap_nodes)),
        }

    def _kpi_public(self, chain_id: str, running: int, total_traps: int) -> dict[str, Any]:
        k = self._state[chain_id]["kpi"]
        return {
            "active_traps": running,
            "total_traps": total_traps,
            "events_total": k["events_total"],
            "attacks": k["attacks"],
            "unique_sources": len(k["sources"]),
            "login_attempts": k["login_attempts"],
        }

    def deploy(self, chain_id: str, action: str) -> dict[str, Any]:
        st = self._init(chain_id)
        chain = store.get(chain_id)
        if action == "deploy":
            st["deployed"] = True
            st["deployed_at"] = time.time()
            for n in (chain.nodes if chain else []):
                if n.type.startswith(("trap/", "endpoint/")):
                    st["node_status"][n.id] = "running"
        else:
            st["deployed"] = False
            for k in list(st["node_status"]):
                st["node_status"][k] = "stopped"
        return self.status(chain_id)

    # ---- генерация живого потока ----
    def _build_attack(self, chain: Chain, rng: random.Random) -> dict[str, Any]:
        res = simulate(chain, SimulateRequest(attacker_profile=rng.choice(
            ["opportunistic", "targeted", "automated"]), seed=rng.randint(0, 10**9)))
        src = _rand_ip(rng)
        cc, country = rng.choice(COUNTRIES)
        tool = rng.choice(TOOLS)
        return {"attack": res, "source_ip": src, "cc": cc, "country": country, "tool": tool,
                "attack_id": "atk_" + format(rng.randint(0, 16**8), "08x")}

    async def stream(self, chain_id: str, rng: random.Random | None = None) -> AsyncIterator[str]:
        rng = rng or random.Random()
        st = self._init(chain_id)

        def sse(obj: dict[str, Any]) -> str:
            return f"data: {json.dumps(obj, ensure_ascii=False)}\n\n"

        # приветственный снимок
        yield sse({"kind": "hello", "status": self.status(chain_id)})
        last_kpi = 0.0
        while True:
            chain = store.get(chain_id)
            if chain is None:
                yield sse({"kind": "bye"})
                return

            if st["deployed"] and chain.nodes:
                a = self._build_attack(chain, rng)
                res = a["attack"]
                steps = res["steps"]
                edges = res["path"]["edges"]
                st["kpi"]["attacks"] += 1
                st["kpi"]["sources"].add(a["source_ip"])
                yield sse({"kind": "attack_start", "attack_id": a["attack_id"],
                           "source_ip": a["source_ip"], "cc": a["cc"], "country": a["country"],
                           "tool": a["tool"], "steps": len(steps)})
                for i, s in enumerate(steps):
                    await asyncio.sleep(rng.uniform(0.6, 1.3))
                    st["kpi"]["events_total"] += 1
                    if s["node_type"] in ("trap/ssh", "trap/rdp") or "креды" in s["detail"]:
                        st["kpi"]["login_attempts"] += 1
                    yield sse({
                        "kind": "hop", "attack_id": a["attack_id"],
                        "edge_id": edges[i - 1] if i > 0 and i - 1 < len(edges) else None,
                        "node_id": s["node_id"], "node_type": s["node_type"],
                        "title": s["title"], "detail": s["detail"],
                        "tactic": s["tactic"], "technique": s["technique"],
                        "is_detection": s["is_detection"],
                        "source_ip": a["source_ip"], "cc": a["cc"], "country": a["country"],
                        "severity": "critical" if s["is_detection"] else rng.choice(["low", "medium", "high"]),
                        "ts": time.time(),
                    })
                yield sse({"kind": "attack_end", "attack_id": a["attack_id"],
                           "reached": res["summary"]["reached"],
                           "detected": res["summary"]["detections"] > 0,
                           "verdict": res["summary"]["verdict"]})

            now = time.time()
            if now - last_kpi > 1.5:
                yield sse({"kind": "kpi", "kpi": self.status(chain_id)["kpi"]})
                last_kpi = now
            await asyncio.sleep(rng.uniform(1.8, 4.0) if st["deployed"] else 2.0)


runtime = RuntimeManager()
