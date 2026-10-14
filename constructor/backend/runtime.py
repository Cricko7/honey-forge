"""Рантайм ловушек и живой поток — РЕАЛЬНЫЙ, поверх бэкенда дашборда HoneyForge.

Enterprise-слой конструктора без синтетики:
- развёртывание доски (deploy) → узлам-ловушкам, привязанным к реальным ловушкам
  дашборда (config._real.id), шлётся настоящая команда start/stop через real_api;
- живой поток: тянем РЕАЛЬНЫЕ события ловушек из `/api/events` бэкенда и отдаём их
  фронту по SSE (`/api/chains/{id}/stream`), сопоставляя событие с узлом доски по trap_id;
- KPI считаются по реальным событиям (события, уникальные источники, попытки входа).

Если бэкенд дашборда недоступен — поток честно молчит (никаких фейковых атак).
"""
from __future__ import annotations

import asyncio
import json
import time
from typing import Any, AsyncIterator

from . import real_api
from .store import store


def _real_id(node: Any) -> str | None:
    """trap_id реальной ловушки, к которой привязан узел доски (или None)."""
    cfg = getattr(node, "config", None) or {}
    real = cfg.get("_real") if isinstance(cfg, dict) else None
    return real.get("id") if isinstance(real, dict) else None


# Тип события бэкенда → попытка аутентификации (для KPI «попытки входа»).
_AUTH_EVENTS = {"service.auth_attempt", "tcp.payload_received"}


class RuntimeManager:
    """Состояние развёртывания и KPI по каждой доске (in-memory)."""

    def __init__(self) -> None:
        self._state: dict[str, dict[str, Any]] = {}

    def _init(self, chain_id: str) -> dict[str, Any]:
        return self._state.setdefault(chain_id, {
            "deployed": False,
            "deployed_at": None,
            "node_status": {},        # node_id -> "running" | "stopped" | "error"
            "seen_events": set(),     # event_id, чтобы не дублировать в потоке
            "kpi": {"events_total": 0, "sources": set(), "login_attempts": 0},
        })

    # ---- узлы доски, привязанные к реальным ловушкам ----
    def _bound_nodes(self, chain_id: str) -> list[Any]:
        chain = store.get(chain_id)
        return [n for n in (chain.nodes if chain else []) if _real_id(n)]

    def _trap_to_node(self, chain_id: str) -> dict[str, str]:
        return {_real_id(n): n.id for n in self._bound_nodes(chain_id)}

    def status(self, chain_id: str) -> dict[str, Any]:
        st = self._init(chain_id)
        bound = self._bound_nodes(chain_id)
        running = sum(1 for n in bound if st["node_status"].get(n.id) == "running")
        return {
            "deployed": st["deployed"],
            "deployed_at": st["deployed_at"],
            "node_status": st["node_status"],
            "kpi": self._kpi_public(chain_id, running, len(bound)),
        }

    def _kpi_public(self, chain_id: str, running: int, total_traps: int) -> dict[str, Any]:
        k = self._state[chain_id]["kpi"]
        return {
            "active_traps": running,
            "total_traps": total_traps,
            "events_total": k["events_total"],
            "attacks": k["events_total"],          # атаки = реальные события
            "unique_sources": len(k["sources"]),
            "login_attempts": k["login_attempts"],
        }

    def deploy(self, chain_id: str, action: str) -> dict[str, Any]:
        """Развернуть/остановить доску — РЕАЛЬНЫЕ команды привязанным ловушкам."""
        st = self._init(chain_id)
        bound = self._bound_nodes(chain_id)
        results: list[dict[str, Any]] = []
        cmd = "start" if action == "deploy" else "stop"
        for n in bound:
            trap_id = _real_id(n)
            try:
                real_api.send_command(trap_id, cmd)
                st["node_status"][n.id] = "running" if cmd == "start" else "stopped"
                results.append({"node_id": n.id, "trap_id": trap_id, "ok": True})
            except real_api.RealApiError as e:
                st["node_status"][n.id] = "error"
                results.append({"node_id": n.id, "trap_id": trap_id, "ok": False, "error": str(e)})
        st["deployed"] = action == "deploy"
        st["deployed_at"] = time.time() if st["deployed"] else None
        out = self.status(chain_id)
        out["commands"] = results
        return out

    # ---- живой поток РЕАЛЬНЫХ событий ----
    async def stream(self, chain_id: str, _rng: Any = None) -> AsyncIterator[str]:
        st = self._init(chain_id)

        def sse(obj: dict[str, Any]) -> str:
            return f"data: {json.dumps(obj, ensure_ascii=False)}\n\n"

        yield sse({"kind": "hello", "status": self.status(chain_id)})
        last_kpi = 0.0
        while True:
            if store.get(chain_id) is None:
                yield sse({"kind": "bye"})
                return

            if st["deployed"]:
                trap_node = self._trap_to_node(chain_id)
                try:
                    events = real_api.list_events(50)
                except real_api.RealApiError:
                    events = []
                # свежие сверху → идём с конца, чтобы отдавать в хронологии
                for ev in reversed(events):
                    eid = ev.get("event_id")
                    if not eid or eid in st["seen_events"]:
                        continue
                    node_id = trap_node.get(ev.get("trap_id"))
                    if node_id is None:
                        continue            # событие не по нашей доске — пропускаем
                    st["seen_events"].add(eid)
                    etype = ev.get("event_type", "")
                    src_ip = (ev.get("source") or {}).get("ip", "—")
                    dst = ev.get("destination") or {}
                    proto = dst.get("protocol") or ""
                    st["kpi"]["events_total"] += 1
                    st["kpi"]["sources"].add(src_ip)
                    if etype in _AUTH_EVENTS:
                        st["kpi"]["login_attempts"] += 1
                    yield sse({
                        "kind": "hop",
                        "attack_id": (ev.get("session_id") or eid)[:12],
                        "edge_id": None,
                        "node_id": node_id,
                        "node_type": "real/" + str(ev.get("type_id", "")),
                        "title": etype or "событие ловушки",
                        "detail": f"{proto}:{dst.get('port', '')}".strip(":"),
                        "tactic": "",
                        "technique": "—",
                        "is_detection": etype in _AUTH_EVENTS,
                        "source_ip": src_ip,
                        "cc": "",
                        "country": "",
                        "severity": "high" if etype in _AUTH_EVENTS else "low",
                        "ts": time.time(),
                    })

            now = time.time()
            if now - last_kpi > 1.5:
                yield sse({"kind": "kpi", "kpi": self.status(chain_id)["kpi"]})
                last_kpi = now
            await asyncio.sleep(2.0)


runtime = RuntimeManager()
