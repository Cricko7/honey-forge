"""Real deployments and telemetry; the simulator is never used for live events."""
import asyncio
import json
import hashlib
import threading
import time
import uuid
from urllib.parse import urlencode

from fastapi import HTTPException
from . import real_api
from .custom import compile_chain
from .http_pipeline import validate_destinations, connect_destinations
from .store import store


def trap_id(chain, node):
    if node.type == 'custom/listener':
        return chain.bindings.get('__custom__', {}).get('trap_id')
    if node.type == 'http/input':
        return chain.bindings.get('__http__', {}).get('trap_id')
    binding = node.config.get('_real', {})
    if not isinstance(binding, dict):
        raise HTTPException(422, 'Invalid trap binding')
    value = chain.bindings.get(node.id, {}).get('trap_id') or binding.get('id')
    if value:
        try:
            return str(uuid.UUID(value))
        except (ValueError, TypeError, AttributeError) as error:
            raise HTTPException(422, 'Invalid trap identifier') from error
    return None


class RuntimeManager:
    def __init__(self):
        self.lock = threading.RLock()

    def status(self, chain_id):
        chain = store.get(chain_id)
        states, traps = {}, {}
        for node in chain.nodes:
            tid = trap_id(chain, node)
            if not tid:
                continue
            try:
                trap = real_api._request('GET', '/api/traps/' + tid)
                states[node.id] = trap['runtime_state']
                traps[node.id] = trap
            except real_api.RealApiError as error:
                if error.status != 404:
                    raise
                states[node.id] = 'deleted'
        return {'deployed': bool(traps), 'node_status': states, 'traps': traps,
                'kpi': {'active_traps': len({t['id'] for t in traps.values() if t['runtime_state'] == 'running'}), 'total_traps': len({t['id'] for t in traps.values()})}}

    def deploy(self, chain_id, action):
        with self.lock, store._lock:
            chain = store.get(chain_id)
            nodes = [n for n in chain.nodes if not n.type.startswith(('custom/', 'http/')) and trap_id(chain, n)]
            compiled_ids = ('__custom__', '__http__')
            if action == 'deploy':
                from .http_builder import compile_http
                # Compile both builders before any side effects, including on mixed boards.
                tcp = compile_chain(chain.model_copy(update={'nodes': [n for n in chain.nodes if not n.type.startswith('http/')]}))
                http = compile_http(chain)
                nodes.extend(n for n in (tcp, http) if n)
            elif action == 'stop':
                from .models import Node
                for key, kind in [('__custom__', 'custom-tcp'), ('__http__', 'custom-http')]:
                    if chain.bindings.get(key, {}).get('trap_id'):
                        nodes.append(Node(id=key, type='build/' + kind))
            if not nodes:
                raise HTTPException(422, 'Добавьте блок реальной ловушки из каталога')
            if action == 'deploy':
                catalog = {e['type_id']: e for e in real_api.list_trap_types() if e['available_for_new_profiles']}
                configs, ports = {}, set()
                # Validate all new blocks before the first side effect.
                for node in nodes:
                    if trap_id(chain, node) and node.id not in compiled_ids:
                        continue
                    type_id = node.type.removeprefix('build/')
                    if type_id not in catalog:
                        raise HTTPException(422, 'Тип ловушки недоступен')
                    configs[node.id] = node.config['compiled']
                    for listener in configs[node.id].get('listeners', configs[node.id].get('services', [])):
                        if listener['port'] in ports:
                            raise HTTPException(422, 'Блоки ловушек используют одинаковый TCP-порт')
                        ports.add(listener['port'])
                if '__http__' in configs:
                    validate_destinations(configs['__http__'],chain.bindings.get('__http__',{}).get('trap_id'))
                for node in nodes:
                    tid = trap_id(chain, node)
                    if tid:
                        if node.id in compiled_ids:
                            digest = hashlib.sha256(json.dumps(configs[node.id], sort_keys=True).encode()).hexdigest()
                            binding = chain.bindings[node.id]
                            if binding.get('digest') != digest:
                                profile = real_api._request('GET', '/api/profiles/' + binding['profile_id'])
                                profile = real_api._request('PATCH', '/api/profiles/' + binding['profile_id'], {'config': configs[node.id]},
                                    {'If-Match': '"profile:' + profile['id'] + ':' + str(profile['revision']) + '"'})
                                binding['digest'] = digest
                                binding['apply_revision'] = str(profile['revision'])
                                store.save(chain)
                            if binding.get('apply_revision'):
                                if not binding.get('apply_command'):
                                    command = real_api.send_command(tid, 'apply_config', {'profile_revision': int(binding['apply_revision'])})
                                    binding['apply_command'] = command['id']
                                    store.save(chain)
                                deadline = time.monotonic() + 20
                                while True:
                                    command = real_api._request('GET', '/api/traps/' + tid + '/commands/' + binding['apply_command'])
                                    if command['status'] == 'succeeded':
                                        break
                                    if command['status'] in ('failed', 'expired'):
                                        binding.pop('apply_command')
                                        store.save(chain)
                                        raise HTTPException(409, 'Не удалось применить конфигурацию; проверьте ловушку и повторите развёртывание')
                                    if time.monotonic() > deadline:
                                        raise HTTPException(503, 'Конфигурация ещё применяется; повторите развёртывание после завершения команды')
                                    time.sleep(0.2)
                                binding.pop('apply_revision')
                                binding.pop('apply_command')
                                store.save(chain)
                        current = real_api._request('GET', '/api/traps/' + tid)
                        if current.get('desired_state') != 'running':
                            real_api.send_command(tid, 'start')
                        continue
                    binding = chain.bindings.setdefault(node.id, {'profile_request': str(uuid.uuid4()), 'trap_request': str(uuid.uuid4())})
                    store.save(chain)
                    type_id = node.type.removeprefix('build/')
                    name = (node.label or chain.name + ' ' + type_id)[:100]
                    if not binding.get('profile_id'):
                        profile = real_api._request('POST', '/api/profiles', {'request_id': binding['profile_request'], 'name': name,
                            'type_id': type_id, 'type_version': catalog[type_id]['type_version'], 'config': configs[node.id]})
                        binding['profile_id'] = profile['id']
                        binding['digest'] = hashlib.sha256(json.dumps(configs[node.id], sort_keys=True).encode()).hexdigest()
                        store.save(chain)
                    digest = hashlib.sha256(json.dumps(configs[node.id], sort_keys=True).encode()).hexdigest()
                    if binding.get('digest') != digest:
                        profile = real_api._request('GET', '/api/profiles/' + binding['profile_id'])
                        real_api._request('PATCH', '/api/profiles/' + binding['profile_id'], {'config': configs[node.id]},
                            {'If-Match': '"profile:' + profile['id'] + ':' + str(profile['revision']) + '"'})
                        binding['digest'] = digest
                        store.save(chain)
                    trap = real_api._request('POST', '/api/traps', {'request_id': binding['trap_request'], 'name': name, 'profile_id': binding['profile_id']})
                    binding['trap_id'] = trap['id']
                    binding['digest'] = hashlib.sha256(json.dumps(configs[node.id], sort_keys=True).encode()).hexdigest()
                    node.config.pop('password', None)
                    store.save(chain)
                if '__http__' in configs:
                    connect_destinations(configs['__http__'],chain.bindings['__http__']['trap_id'])
            else:
                for node in nodes:
                    tid = trap_id(chain, node)
                    if tid:
                        real_api.send_command(tid, 'stop')
            return self.status(chain_id)

    async def stream(self, chain_id):
        seen = set()
        initialized = set()

        def sse(value):
            return 'data: ' + json.dumps(value, ensure_ascii=False) + '\n\n'

        while store.get(chain_id):
            # Session expiry and revocation are checked during a long-lived stream too.
            await asyncio.to_thread(real_api._request, 'GET', '/api/session')
            status = await asyncio.to_thread(self.status, chain_id)
            chain = store.get(chain_id)
            pages = {}

            for tid in dict.fromkeys(trap_id(chain, node) for node in chain.nodes):
                if not tid:
                    continue

                pages[tid] = await asyncio.to_thread(real_api._request, 'GET', '/api/events?' + urlencode({'trap_id': tid, 'limit': 100}))
                if tid not in initialized:
                    # Opening/reconnecting establishes a baseline, not a replay animation.
                    seen.update(event['event_id'] for event in pages[tid]['items'])
                    initialized.add(tid)

            yield sse({'kind': 'hello', 'status': status})

            for node in chain.nodes:
                tid = trap_id(chain, node)
                if not tid:
                    continue

                page = pages[tid]
                for event in reversed(page['items']):
                    if node.type in ('custom/listener', 'http/input') and event.get('destination', {}).get('port') != node.config['port']:
                        continue
                    if event['event_id'] in seen:
                        continue
                    if event['event_type'] in ('http.block','http.custom','http.pipeline_received'):
                        # Event lists intentionally omit payload; details identify the behavior block.
                        event = await asyncio.to_thread(real_api._request, 'GET', '/api/events/' + event['event_id'])
                    seen.add(event['event_id'])
                    event_node = event.get('data', {}).get('node_id', node.id) if event['event_type'] in ('http.block','http.custom','http.pipeline_received') else node.id
                    yield sse({'kind': 'hop', 'node_id': event_node, 'title': event['event_type'], 'detail': event['occurred_at'],
                               'source_ip': event.get('source', {}).get('ip', ''), 'country': '', 'technique': '—',
                               'severity': event.get('data',{}).get('severity','medium'), 'is_detection': True, 'event_id': event['event_id']})
            # Retain only the current polling windows; clearing all IDs replays old events.
            seen.intersection_update(event['event_id'] for page in pages.values() for event in page['items'])
            initialized.intersection_update(pages)

            await asyncio.sleep(2)


runtime = RuntimeManager()
