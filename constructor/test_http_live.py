"""Live acceptance: Python HTTP flow -> ordinary managed agent -> persisted events."""
import json
from pathlib import Path
import secrets
import socket
import ssl
import subprocess
import tempfile
import time
import uuid

import httpx


def main():
    with tempfile.TemporaryDirectory() as tmp:
        ca = Path(tmp) / 'ca.crt'
        subprocess.run(['docker', 'cp', 'honey-forge-web-1:/etc/nginx/certs/ca.crt', str(ca)], check=True, capture_output=True)
        with httpx.Client(base_url='https://localhost:8443', verify=ssl.create_default_context(cafile=str(ca)), trust_env=False, timeout=30) as client:
            headers = {'Origin': 'https://localhost:8443'}

            def request(method, path, body=None, extra_headers=None):
                response = client.request(method, path, json=body, headers={**headers, **(extra_headers or {})})
                if not response.is_success:
                    raise RuntimeError(f'{method} {path}: {response.status_code} {response.text}')
                return response.json() if response.content else None

            session = request('POST', '/api/registrations', {'email': 'http-acceptance-' + uuid.uuid4().hex[:12] + '@example.com', 'password': secrets.token_urlsafe(24),
                'organization': {'mode': 'create', 'name': 'HTTP constructor acceptance'}})
            headers['X-CSRF-Token'] = session['csrf_token']
            chain = request('POST', '/constructor/api/chains', {'name': 'HTTP JSON flow acceptance'})
            ports = []
            for _ in range(2):
                with socket.socket() as probe:
                    probe.bind(('127.0.0.1', 0))
                    ports.append(probe.getsockname()[1])
            nodes = [
                {'id': 'web', 'type': 'http/input', 'config': {'name': 'web', 'port': ports[0], 'method': 'POST', 'path': '/login'}},
                {'id': 'parse', 'type': 'http/parse', 'config': {'format': 'json'}},
                {'id': 'check', 'type': 'http/condition', 'config': {'source': 'json.action', 'operator': 'equals', 'value': 'login'}},
                {'id': 'call', 'type': 'http/call', 'config': {'target': 'other', 'payload': '{"user":"${json.user}"}', 'key': 'result'}},
                {'id': 'other', 'type': 'http/input', 'config': {'name': 'other', 'port': ports[1], 'method': 'ANY', 'path': '*'}},
                {'id': 'upper', 'type': 'http/transform', 'config': {'key': 'user', 'value': '"${json.user}"', 'operation': 'upper'}},
                {'id': 'ok', 'type': 'http/response', 'config': {'format': 'json', 'body': '{"user":"${vars.user}"}', 'status': 200, 'headers': '{}'}},
                {'id': 'final', 'type': 'http/response', 'config': {'format': 'json', 'body': '"${vars.result}"', 'status': 200, 'headers': '{"X-Flow":"custom"}'}},
                {'id': 'no', 'type': 'http/response', 'config': {'format': 'json', 'body': '{"error":"unknown action"}', 'status': 422, 'headers': '{}'}},
            ]
            edges = [{'source': a, 'source_port': p, 'target': b, 'target_port': 'in'} for a, p, b in [
                ('web', 'out', 'parse'), ('parse', 'out', 'check'), ('check', 'yes', 'call'),
                ('check', 'no', 'no'), ('call', 'out', 'final'), ('other', 'out', 'upper'), ('upper', 'out', 'ok')]]
            request('PATCH', '/constructor/api/chains/' + chain['id'], {'nodes': nodes, 'edges': edges})
            runtime_path = '/constructor/api/chains/' + chain['id'] + '/runtime'
            deploy_path = '/constructor/api/chains/' + chain['id'] + '/deploy'
            tid = None
            try:
                request('POST', deploy_path, {'action': 'deploy'})
                deadline = time.monotonic() + 180
                while True:
                    status = request('GET', runtime_path)
                    if status['kpi']['active_traps'] == 1 and all(t['connectivity'] == 'online' for t in status['traps'].values()):
                        break
                    if time.monotonic() >= deadline:
                        raise RuntimeError('HTTP trap did not start: ' + json.dumps(status))
                    time.sleep(2)
                tid = status['traps']['web']['id']
                print('Python HTTP managed trap running on both ports', flush=True)
                with httpx.Client(trust_env=False, timeout=15) as attacker:
                    response = attacker.post(f'http://127.0.0.1:{ports[0]}/login', json={'action': 'login', 'user': 'Alice'})
                    assert response.status_code == 200, response.text
                    assert response.json() == {'user': 'ALICE'}, response.text
                    assert response.headers['x-flow'] == 'custom'
                    assert 'application/json' in response.headers['content-type']
                    rejected = attacker.post(f'http://127.0.0.1:{ports[0]}/login', json={'action': 'unknown'})
                    assert rejected.status_code == 422 and rejected.json()['error'] == 'unknown action', rejected.text
                deadline = time.monotonic() + 40
                while True:
                    events = request('GET', '/api/events?trap_id=' + tid + '&limit=100')['items']
                    kinds = {e['event_type'] for e in events}
                    visited = {request('GET', '/api/events/' + e['event_id'])['data'].get('node_id') for e in events if e['event_type'] == 'http.block'}
                    if {'service.connection_opened', 'http.request', 'http.block', 'http.response', 'service.connection_closed'} <= kinds and {'web', 'other', 'upper', 'final', 'no'} <= visited:
                        break
                    if time.monotonic() >= deadline:
                        raise RuntimeError('Missing HTTP telemetry: ' + json.dumps(events))
                    time.sleep(1)
                print('JSON call result and rejected branch verified; HTTP events persisted on main server', flush=True)
                with client.stream('GET', '/constructor/api/chains/' + chain['id'] + '/stream', headers=headers) as response:
                    response.raise_for_status()
                    highlighted = set()
                    triggered = False
                    for line in response.iter_lines():
                        if line.startswith('data: '):
                            hop = json.loads(line[6:])
                            if hop['kind'] == 'hello' and not triggered:
                                triggered = True
                                # History is a baseline; verify fresh requests after subscribing.
                                with httpx.Client(trust_env=False, timeout=15) as attacker:
                                    accepted = attacker.post(f'http://127.0.0.1:{ports[0]}/login', json={'action': 'login', 'user': 'Alice'})
                                    rejected = attacker.post(f'http://127.0.0.1:{ports[0]}/login', json={'action': 'unknown'})
                                    assert accepted.status_code == 200 and rejected.status_code == 422

                            if hop['kind'] == 'hop':
                                highlighted.add(hop['node_id'])
                                if {'upper', 'final', 'no'} <= highlighted:
                                    break
                print('Board stream highlights executed behavior blocks', flush=True)
                request('POST', deploy_path, {'action': 'deploy'})
                assert len(request('GET', '/api/traps?limit=100')['items']) == 1, 'Repeated deploy created a duplicate'
                request('POST', deploy_path, {'action': 'stop'})
                deadline = time.monotonic() + 30
                while request('GET', runtime_path)['kpi']['active_traps']:
                    if time.monotonic() >= deadline:
                        raise RuntimeError('Stop did not finish')
                    time.sleep(1)
                print('Idempotent deploy and ordinary Stop verified', flush=True)
            finally:
                if tid:
                    current = request('GET', '/api/traps/' + tid)
                    request('DELETE', '/api/traps/' + tid, extra_headers={'X-Expected-Revision': str(current['revision'])})
                request('DELETE', '/constructor/api/chains/' + chain['id'])


if __name__ == '__main__':
    main()
