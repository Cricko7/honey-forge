"""Серверный клиент к РЕАЛЬНОМУ бэкенду HoneyForge (контракт OpenAPI 0.1.0).

Конструктор-модуль обслуживается со своего origin (:8077), а реальный API —
с другого (:8080 стаб / Go-бэк). Прямые fetch из фронта упёрлись бы в CORS,
поэтому ходим в реальный API здесь, на сервере, и отдаём данные фронту через
прокси-эндпоинты в app.py.

Без сторонних зависимостей — только stdlib urllib. Держим cookie-сессию и
CSRF-токен, чтобы слать команды ловушкам (start/stop/apply_config). База и
учётка настраиваются переменными окружения HONEYFORGE_API_BASE / _EMAIL / _PASSWORD.
"""
from __future__ import annotations

import http.cookiejar
import json
import os
import urllib.error
import urllib.request
from typing import Any

API_BASE = os.environ.get("HONEYFORGE_API_BASE", "http://127.0.0.1:8080").rstrip("/")
# Origin должен входить в BROWSER_ORIGINS бэкенда (иначе отклонит мутацию).
ORIGIN = os.environ.get("HONEYFORGE_ORIGIN", "http://127.0.0.1:3000")
_EMAIL = os.environ.get("HONEYFORGE_EMAIL", "admin@perimetr.ru")
_PASSWORD = os.environ.get("HONEYFORGE_PASSWORD", "demo-password-2026")
_TIMEOUT = 4.0

_jar = http.cookiejar.CookieJar()
_opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(_jar))
_csrf: str | None = None


class RealApiError(RuntimeError):
    """Реальный бэкенд недоступен или вернул ошибку."""


def _request(method: str, path: str, body: dict[str, Any] | None = None,
             headers: dict[str, str] | None = None) -> Any:
    url = f"{API_BASE}{path}"
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if method != "GET":
        req.add_header("Origin", ORIGIN)
    for k, v in (headers or {}).items():
        req.add_header(k, v)
    try:
        with _opener.open(req, timeout=_TIMEOUT) as resp:
            raw = resp.read()
            return json.loads(raw) if raw else None
    except urllib.error.HTTPError as e:
        raise RealApiError(f"{method} {path} → HTTP {e.code}") from e
    except (urllib.error.URLError, TimeoutError, ConnectionError) as e:
        raise RealApiError(f"{method} {path} → недоступен ({e})") from e


def _ensure_session() -> None:
    """Залогиниться (если ещё нет csrf), чтобы получить cookie-сессию и CSRF."""
    global _csrf
    if _csrf:
        return
    s = _request("POST", "/api/sessions", {"email": _EMAIL, "password": _PASSWORD})
    _csrf = (s or {}).get("csrf_token") if isinstance(s, dict) else None


def health() -> bool:
    """Доступен ли реальный бэкенд (пробуем лёгкий GET каталога)."""
    try:
        _request("GET", "/api/trap-types?limit=1")
        return True
    except RealApiError:
        return False


def list_trap_types() -> list[dict[str, Any]]:
    page = _request("GET", "/api/trap-types?limit=100")
    return page.get("items", []) if isinstance(page, dict) else []


def list_traps() -> list[dict[str, Any]]:
    page = _request("GET", "/api/traps?limit=100")
    return page.get("items", []) if isinstance(page, dict) else []


def list_profiles() -> list[dict[str, Any]]:
    page = _request("GET", "/api/profiles?limit=100")
    return page.get("items", []) if isinstance(page, dict) else []


def list_events(limit: int = 50) -> list[dict[str, Any]]:
    """Реальные события ловушек с бэкенда дашборда (свежие сверху)."""
    page = _request("GET", f"/api/events?limit={int(limit)}")
    return page.get("items", []) if isinstance(page, dict) else []


def send_command(trap_id: str, action: str, params: dict[str, Any] | None = None) -> dict[str, Any]:
    """Отправить команду ловушке (start | stop | apply_config)."""
    import uuid
    _ensure_session()
    headers = {"X-CSRF-Token": _csrf} if _csrf else {}
    body = {"request_id": str(uuid.uuid4()), "action": action, "params": params or {}}
    return _request("POST", f"/api/traps/{trap_id}/commands", body, headers)


def create_trap(name: str, profile_id: str, description: str = "") -> dict[str, Any]:
    """Создать РЕАЛЬНУЮ ловушку на дашборде (по профилю). Возвращает объект ловушки."""
    _ensure_session()
    headers = {"X-CSRF-Token": _csrf} if _csrf else {}
    body = {"name": name, "profile_id": profile_id, "description": description}
    return _request("POST", "/api/traps", body, headers)
