"""Real constructor/API acceptance. Keeps the requested demo boards for inspection."""
import argparse
from concurrent.futures import ThreadPoolExecutor
import json
from pathlib import Path
import ssl
import subprocess
import time
import uuid

import httpx

from .pipeline_fixtures import collector, processor, coordinator


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--credentials', required=True)
    parser.add_argument('--ca', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    output = Path(args.output)
    output.mkdir(parents=True, exist_ok=True)
    credentials = json.loads(Path(args.credentials).read_text(encoding='utf-8'))
    results = {'boards': [], 'checks': []}
    headers = {'Origin': 'https://localhost:8443'}

    with httpx.Client(base_url='https://localhost:8443', verify=ssl.create_default_context(cafile=args.ca), trust_env=False, timeout=30) as client:
        def request(method, path, body=None):
            response = client.request(method, path, json=body, headers=headers)
            response.raise_for_status()
            return response.json() if response.content else None

        session = request('POST', '/api/sessions', credentials)
        headers['X-CSRF-Token'] = session['csrf_token']

        def check(name, detail):
            results['checks'].append({'name': name, 'detail': detail, 'passed': True})
            (output / 'results.json').write_text(json.dumps(results, ensure_ascii=False, indent=2), encoding='utf-8')
            print(name + ': OK', flush=True)

        def wait(predicate, timeout=180):
            deadline = time.monotonic() + timeout
            while time.monotonic() < deadline:
                value = predicate()
                if value:
                    return value
                time.sleep(1)
            raise RuntimeError('Acceptance timed out')

        def deploy(name, fixture):
            existing = request('GET', '/constructor/api/chains')
            chain = next((board for board in existing if board['name'] == name), None)
            if chain is None:
                chain = request('POST', '/constructor/api/chains', {'name': name, 'description': 'Реальный тест надёжной HTTP-цепочки. Создано через API.', 'tags': ['pipeline', 'demo', 'reliability']})
            nodes, edges = fixture
            saved = request('PATCH', '/constructor/api/chains/' + chain['id'], {'nodes': nodes, 'edges': edges, 'viewport': {'x': 40, 'y': 130, 'zoom': 0.42}})
            (output / (chain['id'] + '.json')).write_text(json.dumps(saved, ensure_ascii=False, indent=2), encoding='utf-8')
            request('POST', '/constructor/api/chains/' + chain['id'] + '/deploy', {'action': 'deploy'})
            status = wait(lambda: (s if s['kpi']['active_traps'] == 1 and all(t['connectivity'] == 'online' for t in s['traps'].values()) else None)
                          if (s := request('GET', '/constructor/api/chains/' + chain['id'] + '/runtime')) else None)
            trap = status['traps']['web']['id']

            def listener_ready():
                entry = next(n['config'] for n in nodes if n['id'] == 'web')
                try:
                    with httpx.Client(trust_env=False, timeout=2) as probe:
                        response = probe.get('http://127.0.0.1:' + str(entry['port']) + entry['path'])
                        return response.status_code == 405
                except httpx.HTTPError:
                    return False

            wait(listener_ready, timeout=90)

            results['boards'].append({'name': name, 'chain_id': chain['id'], 'trap_id': trap, 'node_count': len(nodes), 'edge_count': len(edges)})
            print(name + ': running', flush=True)
            return trap

        c = deploy('C — Сбор результатов и оценка риска', collector(32183))
        b = deploy('B — Преобразование и маршрутизация', processor(32182, c))
        a = deploy('A — Вход, три ветки, вызов и объединение', coordinator(32181, 32184, b))
        check('Three boards deployed', {'blocks': sum(board['node_count'] for board in results['boards']), 'traps': 3, 'ports': [32181, 32182, 32183, 32184]})

        def attack(run, score=70, action='login'):
            with httpx.Client(trust_env=False, timeout=20) as attacker:
                response = attacker.post('http://127.0.0.1:32181/login', json={'action': action, 'user': 'Alice', 'score': score, 'run': run})
                response.raise_for_status()
                payload = response.json()
                assert response.status_code == 202 and payload['results']['branch3']['body']['user'] == 'ALICE', payload
                return payload

        def custom_events(trap):
            page = request('GET', '/api/events?trap_id=' + trap + '&event_type=http.custom&limit=100')
            return [request('GET', '/api/events/' + item['event_id']) for item in page['items']]

        def find(trap, run, name):
            return next((e for e in custom_events(trap) if e['data']['name'] == name and e['data']['payload'].get('run') == run), None)

        high = 'high-' + uuid.uuid4().hex
        response = attack(high)
        assert response['results']['branch1']['status'] == 202
        event = wait(lambda: find(c, high, 'collector.alert'))
        assert event['data']['payload']['user'] == 'ALICE'
        check('High risk A to B to C', {'run': high, 'response': response, 'collector_event': event})

        low = 'low-' + uuid.uuid4().hex
        attack(low, score=10)
        event = wait(lambda: find(c, low, 'collector.observe'))
        check('Low risk branch', {'run': low, 'collector_event': event})

        with httpx.Client(trust_env=False, timeout=15) as attacker:
            bad = attacker.post('http://127.0.0.1:32181/login', content='{', headers={'Content-Type': 'application/json'})
            denied = attacker.post('http://127.0.0.1:32181/login', json={'action': 'unknown'})
            assert bad.status_code == denied.status_code == 422
        check('Malformed JSON and rejected action', 'Both return 422 with the expected error response')

        with httpx.Client(base_url='https://localhost:8443', verify=ssl.create_default_context(cafile=args.ca), trust_env=False) as anonymous:
            assert anonymous.get('/api/traps/' + c).status_code == 401
        check('Unauthenticated control access rejected', 'GET trap returns 401 without an operator session')

        def command(trap, action):
            cmd = request('POST', '/api/traps/' + trap + '/commands', {'request_id': str(uuid.uuid4()), 'action': action, 'params': {}})
            result = wait(lambda: (v if v['status'] in ('succeeded', 'failed', 'expired') else None)
                          if (v := request('GET', '/api/traps/' + trap + '/commands/' + cmd['id'])) else None, timeout=90)
            assert result['status'] == 'succeeded', result

        command(b, 'stop')
        pending = 'restart-' + uuid.uuid4().hex
        try:
            response = attack(pending)
            assert response['results']['branch1']['status'] == 202, response
            assert not find(c, pending, 'collector.alert')
            subprocess.run(['docker', 'compose', 'restart', 'api'], check=True, capture_output=True)
            def ready():
                try:
                    return request('GET', '/api/session')
                except httpx.HTTPError:
                    return None

            wait(ready, timeout=45)
        finally:
            command(b, 'start')
        event = wait(lambda: find(c, pending, 'collector.alert'))
        check('Queue survives stopped receiver and API restart', {'run': pending, 'collector_event': event})

        request('DELETE', '/api/trap-links/' + a + '/' + b)
        try:
            revoked = 'revoked-' + uuid.uuid4().hex
            response = attack(revoked)
            assert response['results']['branch1']['status'] == 503, response
            assert not find(c, revoked, 'collector.alert')
            check('Revoked link follows error branch', {'run': revoked, 'response': response})
        finally:
            request('POST', '/api/trap-links', {'source_id': a, 'target_id': b})

        runs = ['burst-' + uuid.uuid4().hex for _ in range(24)]
        started = time.monotonic()
        with ThreadPoolExecutor(max_workers=8) as executor:
            responses = list(executor.map(attack, runs))
        assert all(r['results']['branch1']['status'] == 202 for r in responses)
        received = wait(lambda: (items if {e['data']['payload'].get('run') for e in items} >= set(runs) else None)
                        if (items := [e for e in custom_events(c) if e['data']['name'] == 'collector.alert']) else None, timeout=180)
        assert all(request('GET', '/api/traps/' + trap)['runtime_state'] == 'running' for trap in (a, b, c))
        check('Concurrent burst', {'requests': len(runs), 'collector_results': len([e for e in received if e['data']['payload'].get('run') in runs]), 'elapsed_seconds': round(time.monotonic() - started, 2), 'all_traps_running': True})

        print('Acceptance complete. Demo boards remain deployed.', flush=True)


if __name__ == '__main__':
    main()
