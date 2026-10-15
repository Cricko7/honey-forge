"""Реестр портов и полей конфигурации узлов конструктора."""
from __future__ import annotations

from typing import Any, Literal

# Поддерживаемые типы полей инспектора: text, number, select, toggle, textarea, tags, secret
FieldType = Literal["text", "number", "select", "toggle", "textarea", "tags", "secret", "node"]


def field(
    key: str,
    label: str,
    ftype: FieldType,
    default: Any = None,
    *,
    options: list[str] | None = None,
    placeholder: str | None = None,
    hint: str | None = None,
    min: float | None = None,
    max: float | None = None,
) -> dict[str, Any]:
    f: dict[str, Any] = {"key": key, "label": label, "type": ftype, "default": default}
    if options is not None:
        f["options"] = options
    if placeholder is not None:
        f["placeholder"] = placeholder
    if hint is not None:
        f["hint"] = hint
    if min is not None:
        f["min"] = min
    if max is not None:
        f["max"] = max
    return f


def port(pid: str, label: str, kind: Literal["in", "out"], flavor: str = "flow") -> dict[str, Any]:
    """Порт узла. flavor группирует совместимость рёбер: flow / creds / token / loot."""
    return {"id": pid, "label": label, "kind": kind, "flavor": flavor}


# Проектные блоки «защиты сервера» — это топология/дизайн (хранится в конструкторе),
# в отличие от РЕАЛЬНЫХ ловушек, которые приходят из дашборда (категория real/* на фронте).
CATEGORIES = [
    {"id": "infra", "label": "Инфраструктура", "accent": "#4A6CF7"},
    {"id": "defense", "label": "Защита и реакции", "accent": "#B4455E"},
    {"id": "lure", "label": "Приманки", "accent": "#C97A2B"},
]


# Проектные блоки защиты. Реальные ловушки добавляются динамически из дашборда (real/*).
NODE_TYPES: list[dict[str, Any]] = [
    {
        "type": "entry/internet",
        "category": "infra",
        "name": "Интернет",
        "subtitle": "Внешний периметр — корень схемы",
        "icon": "globe",
        "risk": "high",
        "root": True,
        "ports": [port("out", "входящий трафик", "out", "flow")],
        "config": [
            field("public_ip", "Публичный IP", "text", "203.0.113.10"),
            field("domain", "Домен", "text", "corp.example.com"),
        ],
        "doc": "Корень схемы: отсюда идёт весь внешний трафик и потенциальная атака. "
               "От этого узла ведите связи ко всему, что смотрит наружу.",
    },
    {
        "type": "net/firewall",
        "category": "infra",
        "name": "Межсетевой экран",
        "subtitle": "Firewall / фильтрация трафика",
        "icon": "shield",
        "risk": "low",
        "ports": [port("in", "трафик", "in", "flow"), port("out", "разрешённый трафик", "out", "flow")],
        "config": [
            field("policy", "Политика по умолчанию", "select", "deny", options=["deny", "allow"]),
            field("allow_ports", "Открытые порты", "tags", ["443", "80"]),
        ],
        "doc": "Первая линия: пропускает только разрешённый трафик внутрь периметра.",
    },
    {
        "type": "net/waf",
        "category": "infra",
        "name": "WAF",
        "subtitle": "Web Application Firewall",
        "icon": "shield",
        "risk": "low",
        "ports": [port("in", "HTTP-запросы", "in", "flow"), port("out", "очищенные запросы", "out", "flow")],
        "config": [
            field("mode", "Режим", "select", "block", options=["detect", "block"]),
            field("ruleset", "Набор правил", "text", "OWASP CRS 4.0"),
        ],
        "doc": "Фильтрует веб-атаки (SQLi/XSS/RCE) до того, как они дойдут до приложения.",
    },
    {
        "type": "net/segment",
        "category": "infra",
        "name": "Сегмент сети",
        "subtitle": "Зона (DMZ / внутренняя / изолированная)",
        "icon": "network",
        "risk": "low",
        "ports": [port("in", "вход в зону", "in", "flow"), port("out", "ресурсы зоны", "out", "flow")],
        "config": [
            field("name", "Название", "text", "DMZ"),
            field("cidr", "Подсеть", "text", "10.10.20.0/24"),
            field("zone", "Класс зоны", "select", "dmz", options=["dmz", "internal", "restricted"]),
        ],
        "doc": "Группирует ресурсы в зону с общими правилами доступа и сегментацией.",
    },
    {
        "type": "asset/server",
        "category": "infra",
        "name": "Защищаемый сервер",
        "subtitle": "Боевой сервер — то, что защищаем",
        "icon": "monitor",
        "risk": "high",
        "ports": [
            port("in", "доступ к серверу", "in", "flow"),
            port("out", "размещённые данные", "out", "flow"),
        ],
        "config": [
            field("hostname", "Hostname", "text", "app-prod-01"),
            field("ip", "IP-адрес", "text", "10.10.20.10"),
            field("os", "ОС", "select", "Ubuntu 22.04",
                  options=["Ubuntu 22.04", "Debian 12", "RHEL 9", "Windows Server 2022"]),
            field("role", "Роль", "text", "app + file storage"),
        ],
        "doc": "Боевой сервер корпорации. Цель защиты — не дать атакующему добраться до его данных.",
    },
    {
        "type": "asset/file",
        "category": "infra",
        "name": "Важные данные",
        "subtitle": "Критичный файл / секрет",
        "icon": "file",
        "risk": "high",
        "ports": [port("in", "хранится на", "in", "loot")],
        "config": [
            field("filename", "Имя файла", "text", "secret.txt"),
            field("classification", "Гриф", "select", "confidential",
                  options=["public", "internal", "confidential", "secret"]),
            field("note", "Описание", "textarea", "Ключевой корпоративный секрет"),
        ],
        "doc": "Защищаемый актив: файл/секрет корпорации. Доступ к нему — цель атакующего.",
    },
    {
        "type": "asset/db",
        "category": "infra",
        "name": "База данных",
        "subtitle": "Критичная БД",
        "icon": "database",
        "risk": "high",
        "ports": [port("in", "подключение", "in", "flow")],
        "config": [
            field("engine", "Движок", "select", "postgresql", options=["postgresql", "mysql", "mssql", "mongodb"]),
            field("name", "Имя БД", "text", "billing"),
        ],
        "doc": "Критичная база данных с боевыми данными.",
    },
    {
        "type": "defense/soc",
        "category": "defense",
        "name": "SOC / Алерт",
        "subtitle": "Центр мониторинга",
        "icon": "bell",
        "risk": "low",
        "ports": [port("in", "события", "in", "flow")],
        "config": [
            field("severity", "Порог критичности", "select", "high",
                  options=["low", "medium", "high", "critical"]),
            field("channels", "Каналы оповещения", "tags", ["soc-telegram", "email"]),
        ],
        "doc": "Получает сигналы от ловушек/приманок и поднимает инцидент.",
    },
    {
        "type": "defense/siem",
        "category": "defense",
        "name": "SIEM / SOAR",
        "subtitle": "Сбор телеметрии и реакция",
        "icon": "share",
        "risk": "low",
        "ports": [port("in", "телеметрия", "in", "flow")],
        "config": [
            field("endpoint", "Приёмник", "text", "siem.corp.local:514"),
            field("format", "Формат", "select", "CEF", options=["CEF", "syslog", "webhook-json"]),
            field("attack_mapping", "Маппинг на MITRE ATT&CK", "toggle", True),
        ],
        "doc": "Централизует события и запускает автоматические сценарии реагирования.",
    },
    {
        "type": "defense/block",
        "category": "defense",
        "name": "Авто-блокировка",
        "subtitle": "Блок источника атаки",
        "icon": "shield",
        "risk": "low",
        "ports": [port("in", "триггер", "in", "flow")],
        "config": [
            field("action", "Действие", "select", "block-ip", options=["block-ip", "isolate-host", "null-route"]),
            field("duration_min", "На сколько, мин", "number", 60, min=1, max=10080),
        ],
        "doc": "Реакция: блокирует IP/изолирует хост при подтверждённой атаке.",
    },
    {
        "type": "lure/honeytoken",
        "category": "lure",
        "name": "Honeytoken",
        "subtitle": "Ложные креды / API-ключ",
        "icon": "key",
        "risk": "low",
        "ports": [port("in", "размещён на", "in", "flow"), port("out", "сигнал об утечке", "out", "creds")],
        "config": [
            field("kind", "Тип токена", "select", "ssh-keypair",
                  options=["ssh-keypair", "aws-key", "db-connection-string", "domain-cred"]),
            field("username", "Логин-приманка", "text", "svc-backup"),
            field("alert_on_use", "Алерт при использовании", "toggle", True),
        ],
        "doc": "Ложная учётка-приманка. Любое её применение = достоверный сигнал компрометации.",
    },
    {
        "type": "lure/decoy_file",
        "category": "lure",
        "name": "Файл-приманка",
        "subtitle": "Канарейка-документ",
        "icon": "file",
        "risk": "low",
        "ports": [port("in", "лежит на", "in", "flow"), port("out", "открыт / выгружен", "out", "token")],
        "config": [
            field("filename", "Имя файла", "text", "passwords_final.xlsx"),
            field("canary_type", "Тип канарейки", "select", "web-beacon",
                  options=["web-beacon", "dns-callback", "office-macro"]),
        ],
        "doc": "Документ-канарейка рядом с важными данными: сигналит при открытии/выгрузке.",
    },
]

NODE_TYPES_BY_ID: dict[str, dict[str, Any]] = {n["type"]: n for n in NODE_TYPES}


def default_config(node_type: str) -> dict[str, Any]:
    spec = NODE_TYPES_BY_ID.get(node_type)
    if not spec:
        return {}
    return {f["key"]: f.get("default") for f in spec.get("config", [])}


def registry_payload() -> dict[str, Any]:
    return {"categories": CATEGORIES, "node_types": NODE_TYPES}
