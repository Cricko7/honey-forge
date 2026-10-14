"""Live acceptance: assembled multi-port trap -> actual TCP responses -> persisted events."""
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

            def request(method, path, body=None):
                response = client.request(method, path, json=body, headers=headers)
                if not response.is_success:
                    raise RuntimeError(f'{method} {path}: {response.status_code} {response.text}')
                return response.json() if response.content else None

            session = request('POST', '/api/registrations', {'email': 'constructor-' + uuid.uuid4().hex[:12] + '@example.com', 'password': secrets.token_urlsafe(24),
                                                           'organization': {'mode': 'create', 'name': 'Constructor acceptance'}})
            headers['X-CSRF-Token'] = session['csrf_token']
            chain = request('POST', '/constructor/api/chains', {'name': 'Custom mail and admin console'})
            ports = []
            for _ in range(2):
                with socket.socket() as probe:
                    probe.bind(('127.0.0.1', 0))
                    ports.append(probe.getsockname()[1])
            nodes = [
                {'id': 'mail', 'type': 'custom/listener', 'config': {'name': 'mail', 'port': ports[0]}},
                {'id': 'hello', 'type': 'custom/banner', 'config': {'text': '220 Forge Mail\r\n', 'close': False}},
                {'id': 'helo', 'type': 'custom/rule', 'config': {'match': 'prefix', 'input': 'HELO', 'response': '250 Hello\r\n', 'close': False}},
                {'id': 'quit', 'type': 'custom/rule', 'config': {'match': 'exact', 'input': 'QUIT', 'response': '221 Bye\r\n', 'close': True}},
                {'id': 'admin', 'type': 'custom/listener', 'config': {'name': 'admin', 'port': ports[1]}},
                {'id': 'auth', 'type': 'custom/rule', 'config': {'match': 'contains', 'input': 'password', 'response': 'AUTH denied\r\n', 'close': True}},
            ]
            edges = [{'source': a, 'source_port': 'out', 'target': b, 'target_port': 'in'} for a, b in [('mail', 'hello'), ('hello', 'helo'), ('helo', 'quit'), ('admin', 'auth')]]
            request('PATCH', '/constructor/api/chains/' + chain['id'], {'nodes': nodes, 'edges': edges})
            runtime_path = '/constructor/api/chains/' + chain['id'] + '/runtime'
            deploy_path = '/constructor/api/chains/' + chain['id'] + '/deploy'
            request('POST', deploy_path, {'action': 'deploy'})
            deadline = time.monotonic() + 180
            while True:
                status = request('GET', runtime_path)
                if status['kpi']['active_traps'] == 1 and all(t['connectivity'] == 'online' for t in status['traps'].values()):
                    break
                if time.monotonic() >= deadline:
                    raise RuntimeError('Trap did not start: ' + json.dumps(status))
                time.sleep(2)
            print('Real trap running on both ports', flush=True)
            tid = status['traps']['mail']['id']
            with socket.create_connection(('127.0.0.1', ports[0]), timeout=10) as connection:
                banner = connection.makefile('rb').readline()
                assert banner == b'220 Forge Mail\r\n', banner
                connection.sendall(b'HE')
                connection.sendall(b'LO example\r\nQUIT\r\n')
                response = b''
                while data := connection.recv(4096):
                    response += data
                assert response == b'250 Hello\r\n221 Bye\r\n', response
            with socket.create_connection(('127.0.0.1', ports[1]), timeout=10) as connection:
                connection.sendall(b'password=decoy\r\n')
                assert connection.recv(4096) == b'AUTH denied\r\n'
            deadline = time.monotonic() + 40
            while True:
                events = request('GET', '/api/events?trap_id=' + tid + '&limit=100')['items']
                kinds = {e['event_type'] for e in events}
                closed_ports = {e['destination']['port'] for e in events if e['event_type'] == 'tcp.connection_closed'}
                if {'tcp.connection_opened', 'tcp.payload_received', 'tcp.connection_closed'} <= kinds and closed_ports == set(ports):
                    break
                if time.monotonic() >= deadline:
                    raise RuntimeError('Missing events: ' + json.dumps(events))
                time.sleep(1)
            with client.stream('GET', '/constructor/api/chains/' + chain['id'] + '/stream', headers=headers) as response:
                response.raise_for_status()
                found = set()
                for line in response.iter_lines():
                    if line.startswith('data: '):
                        item = json.loads(line[6:])
                        if item['kind'] == 'hop':
                            found.add(item['node_id'])
                            if {'mail', 'admin'} <= found:
                                break
            request('POST', deploy_path, {'action': 'deploy'})
            assert len(request('GET', '/api/traps?limit=100')['items']) == 1, 'Repeated deploy created a duplicate'
            request('POST', deploy_path, {'action': 'stop'})
            deadline = time.monotonic() + 30
            while request('GET', runtime_path)['kpi']['active_traps']:
                if time.monotonic() > deadline:
                    raise RuntimeError('Stop did not finish')
                time.sleep(1)
            nodes[1]['config']['text'] = '220 Updated Mail\r\n'
            request('PATCH', '/constructor/api/chains/' + chain['id'], {'nodes': nodes, 'edges': edges})
            request('POST', deploy_path, {'action': 'deploy'})
            deadline = time.monotonic() + 60
            while True:
                status = request('GET', runtime_path)
                trap = status['traps']['mail']
                if trap['runtime_state'] == 'running' and (trap.get('applied_profile_revision') or 0) >= 2:
                    break
                if time.monotonic() > deadline:
                    raise RuntimeError('Updated configuration not applied: ' + json.dumps(status))
                time.sleep(1)
            with socket.create_connection(('127.0.0.1', ports[0]), timeout=10) as connection:
                assert connection.makefile('rb').readline() == b'220 Updated Mail\r\n'
            request('POST', deploy_path, {'action': 'stop'})
            print(json.dumps({'chain_id': chain['id'], 'trap_id': tid, 'ports': ports, 'event_count': len(events), 'event_types': sorted(kinds),
                              'constructor_stream_nodes': sorted(found), 'duplicate_deploy': 'passed', 'stop': 'passed', 'updated_banner': 'passed'}, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
