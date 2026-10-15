"""Синтетическая симуляция пути атакующего по графу."""
from __future__ import annotations

import random
from typing import Any

from .models import Chain, SimulateRequest

# Условный маппинг типов узлов на технику MITRE ATT&CK и фразу события.
ATTACK_MAP: dict[str, dict[str, str]] = {
    "entry/internet": {"tactic": "Reconnaissance", "technique": "T1595", "verb": "Сканирует периметр"},
    "entry/lan": {"tactic": "Lateral Movement", "technique": "T1021", "verb": "Закрепился во внутренней сети"},
    "trap/ssh": {"tactic": "Credential Access", "technique": "T1110", "verb": "Брутфорсит SSH"},
    "trap/rdp": {"tactic": "Lateral Movement", "technique": "T1021.001", "verb": "Пытается войти по RDP"},
    "trap/smb": {"tactic": "Collection", "technique": "T1039", "verb": "Перебирает сетевые шары"},
    "trap/http-app": {"tactic": "Initial Access", "technique": "T1190", "verb": "Эксплуатирует веб-приложение"},
    "trap/redis": {"tactic": "Discovery", "technique": "T1046", "verb": "Нашёл открытый Redis"},
    "trap/sql": {"tactic": "Collection", "technique": "T1005", "verb": "Дампит базу данных"},
    "endpoint/workstation": {"tactic": "Credential Access", "technique": "T1555", "verb": "Крадёт кэш-креды с АРМ"},
    "endpoint/dc": {"tactic": "Impact", "technique": "T1486", "verb": "Атакует контроллер домена"},
    "lure/honeytoken": {"tactic": "Credential Access", "technique": "T1552", "verb": "Подобрал honeytoken-креды"},
    "lure/document": {"tactic": "Collection", "technique": "T1119", "verb": "Открыл документ-приманку"},
    "logic/condition": {"tactic": "Execution", "technique": "—", "verb": "Ветвление сценария"},
    "logic/delay": {"tactic": "Defense Evasion", "technique": "—", "verb": "Пауза (правдоподобие)"},
    "output/alert": {"tactic": "Detection", "technique": "—", "verb": "Сработал алерт в SOC"},
    "output/siem": {"tactic": "Detection", "technique": "—", "verb": "Событие выгружено в SIEM"},
}

PROFILE_DEPTH = {"opportunistic": 0.55, "targeted": 0.9, "automated": 0.7}


def simulate(chain: Chain, req: SimulateRequest) -> dict[str, Any]:
    rng = random.Random(req.seed if req.seed is not None else random.randint(0, 10**9))
    nodes_by_id = {n.id: n for n in chain.nodes}

    # исходящие рёбра по узлу
    out_edges: dict[str, list] = {}
    for e in chain.edges:
        out_edges.setdefault(e.source, []).append(e)

    # выбор точки входа
    entry_id = req.entry_node_id
    if entry_id not in nodes_by_id:
        entries = [n.id for n in chain.nodes if n.type.startswith("entry/")]
        entry_id = entries[0] if entries else (chain.nodes[0].id if chain.nodes else None)
    if entry_id is None:
        return {"steps": [], "summary": {"reached": 0, "techniques": [], "verdict": "Пустая цепочка"}}

    advance_p = PROFILE_DEPTH.get(req.attacker_profile, 0.7)
    steps: list[dict[str, Any]] = []
    visited: set[str] = set()
    techniques: list[str] = []
    t_ms = 0
    stolen: list[str] = []          # перенесённые honeytoken-креды
    path_nodes: list[str] = []
    path_edges: list[str] = []

    current = entry_id
    step_no = 0
    while current and current not in visited and step_no < 64:
        visited.add(current)
        node = nodes_by_id.get(current)
        if node is None:
            break
        meta = ATTACK_MAP.get(node.type, {"tactic": "Unknown", "technique": "—", "verb": "Действие"})
        delay = node.config.get("ms", 0) if node.type == "logic/delay" else rng.randint(300, 2200)
        t_ms += int(delay)

        # добыча honeytoken-кредов для продвижения дальше
        detail = meta["verb"]
        if node.type == "lure/honeytoken":
            cred = f'{node.config.get("username", "svc")}:{str(node.config.get("secret", "***"))[:6]}…'
            stolen.append(cred)
            detail += f" ({cred})"
        if node.type in ("trap/ssh", "trap/rdp", "endpoint/workstation", "endpoint/dc") and stolen:
            detail += f" — применяет украденные креды: {stolen[-1]}"

        label = node.label or node.type
        steps.append({
            "step": step_no,
            "node_id": current,
            "node_type": node.type,
            "title": label,
            "at_ms": t_ms,
            "tactic": meta["tactic"],
            "technique": meta["technique"],
            "detail": detail,
            "is_detection": node.type.startswith("output/"),
        })
        path_nodes.append(current)
        if meta["technique"] != "—":
            techniques.append(meta["technique"])

        # выбор следующего ребра
        edges = out_edges.get(current, [])
        if not edges:
            break
        # условные узлы ветвятся «как будто» нашли креды
        if node.type == "logic/condition":
            found = bool(stolen) or rng.random() < advance_p
            pick = [e for e in edges if e.source_port == ("out_true" if found else "out_false")]
            edges = pick or edges
        # атакующий может «сдаться» в зависимости от профиля
        if rng.random() > advance_p and not node.type.startswith(("entry/", "logic/")):
            steps.append({
                "step": step_no + 1, "node_id": current, "node_type": node.type,
                "title": label, "at_ms": t_ms + rng.randint(100, 600),
                "tactic": "—", "technique": "—",
                "detail": "Атакующий прекратил активность на этом узле.", "is_detection": False,
            })
            break
        nxt = rng.choice(edges)
        path_edges.append(nxt.id)
        current = nxt.target
        step_no += 1

    uniq_tech = sorted(set(t for t in techniques if t != "—"))
    detections = sum(1 for s in steps if s.get("is_detection"))
    if detections:
        verdict = "Атакующий детектирован и выведен в SIEM"
    else:
        verdict = "Путь пройден без узла-реакции — добавьте алерт/SIEM"
    return {
        "entry_node_id": entry_id,
        "steps": steps,
        "path": {"nodes": path_nodes, "edges": path_edges},
        "summary": {
            "reached": len(path_nodes),
            "duration_ms": t_ms,
            "techniques": uniq_tech,
            "stolen_credentials": stolen,
            "detections": detections,
            "verdict": verdict,
        },
    }
