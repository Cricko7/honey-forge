"""Forward the current operator's session, never a shared login."""
from contextvars import ContextVar
import json
import os
import ssl
import urllib.error
import urllib.request
import uuid

API_BASE = os.environ.get('HONEYFORGE_API_BASE', 'https://api:8443').rstrip('/')
request_headers = ContextVar('request_headers', default={})


class RealApiError(RuntimeError):
    def __init__(self, status, code, message):
        super().__init__(message)
        self.status, self.code = status, code


def _request(method, path, body=None, headers=None):
    req = urllib.request.Request(API_BASE + path, data=json.dumps(body).encode() if body is not None else None, method=method)
    for key, value in {**request_headers.get(), **(headers or {})}.items():
        req.add_header(key, value)
    req.add_header('Content-Type', 'application/json')
    context = ssl.create_default_context(cafile=os.environ.get('HONEYFORGE_CA_FILE'))
    try:
        with urllib.request.urlopen(req, timeout=15, context=context) as response:
            raw = response.read()
            return json.loads(raw) if raw else None
    except urllib.error.HTTPError as error:
        try:
            detail = json.loads(error.read()).get('error', {})
        except (ValueError, AttributeError):
            detail = {}
        raise RealApiError(error.code, detail.get('code', 'backend_error'), detail.get('message', 'HoneyForge rejected the request')) from error
    except (urllib.error.URLError, TimeoutError, ConnectionError) as error:
        raise RealApiError(503, 'service_unavailable', 'HoneyForge is unavailable') from error


def list_all(path):
    from urllib.parse import urlencode
    items, cursor = [], None
    while True:
        query = {'limit': 100}
        if cursor:
            query['cursor'] = cursor
        page = _request('GET', path + '?' + urlencode(query))
        items.extend(page['items'])
        cursor = page.get('next_cursor')
        if not cursor:
            return items


def list_trap_types():
    return list_all('/api/trap-types')


def list_traps():
    return list_all('/api/traps')


def send_command(trap_id, action, params=None):
    return _request('POST', f'/api/traps/{trap_id}/commands', {'request_id': str(uuid.uuid4()), 'action': action, 'params': params or {}})
