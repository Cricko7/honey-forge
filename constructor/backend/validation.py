"""Валидация цепочки ловушек перед развёртыванием.

Проверяет связность attack-path, совместимость портов рёбер, наличие точки входа
и реакций, конфликты портов сервисов. Данные пока условные, но правила реальные —
это то, что отличает «коммерческий» конструктор от игрушки.
"""
from __future__ import annotations

from .models import Chain, ValidationIssue, ValidationResult
from .node_registry import NODE_TYPES_BY_ID


def _port_flavor(node_type: str, port_id: str) -> str | None:
    spec = NODE_TYPES_BY_ID.get(node_type)
    if not spec:
        return None
    for p in spec["ports"]:
        if p["id"] == port_id:
            return p["flavor"]
    return None


def validate_chain(chain: Chain) -> ValidationResult:
    issues: list[ValidationIssue] = []
    nodes_by_id = {n.id: n for n in chain.nodes}

    # 1. Неизвестные типы узлов
    for n in chain.nodes:
        if n.type not in NODE_TYPES_BY_ID:
            issues.append(ValidationIssue(level="error", node_id=n.id,
                                          message=f"Неизвестный тип узла: {n.type}"))

    # 2. Точка входа обязательна
    entries = [n for n in chain.nodes if n.type.startswith("entry/")]
    if not entries:
        issues.append(ValidationIssue(level="error",
                                      message="Нет точки входа — атакующему неоткуда начать путь."))

    # 3. Рёбра: существование концов + совместимость flavor
    for e in chain.edges:
        if e.source not in nodes_by_id:
            issues.append(ValidationIssue(level="error", edge_id=e.id,
                                          message="Ребро ссылается на несуществующий источник."))
            continue
        if e.target not in nodes_by_id:
            issues.append(ValidationIssue(level="error", edge_id=e.id,
                                          message="Ребро ссылается на несуществующую цель."))
            continue
        sf = _port_flavor(nodes_by_id[e.source].type, e.source_port)
        tf = _port_flavor(nodes_by_id[e.target].type, e.target_port)
        if sf is None or tf is None:
            issues.append(ValidationIssue(level="warning", edge_id=e.id,
                                          message="Порт ребра не найден в типе узла."))
        elif sf != tf:
            issues.append(ValidationIssue(level="warning", edge_id=e.id,
                                          message=f"Связь несовместимых портов: {sf} → {tf}."))

    # 4. Висячие узлы (кроме точек входа и реакций)
    connected: set[str] = set()
    for e in chain.edges:
        connected.add(e.source)
        connected.add(e.target)
    for n in chain.nodes:
        if n.id in connected:
            continue
        if n.type.startswith("entry/") or n.type.startswith("output/"):
            continue
        issues.append(ValidationIssue(level="warning", node_id=n.id,
                                      message="Узел ни с чем не связан — недостижим в attack-path."))

    # 5. Конфликт TCP-портов сервисов
    seen_ports: dict[int, str] = {}
    for n in chain.nodes:
        p = n.config.get("port")
        if isinstance(p, int):
            if p in seen_ports and seen_ports[p] != n.id:
                issues.append(ValidationIssue(level="error", node_id=n.id,
                                              message=f"Конфликт TCP-порта {p} с другим узлом."))
            seen_ports[p] = n.id

    # 6. Нет ни одной реакции — события некуда девать
    if not any(n.type.startswith("output/") for n in chain.nodes):
        issues.append(ValidationIssue(level="info",
                                      message="Нет узла-реакции (алерт/SIEM) — события не будут вынесены."))

    ok = not any(i.level == "error" for i in issues)
    return ValidationResult(ok=ok, issues=issues)
