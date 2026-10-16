"""Ten real traps and two independent constructor boards."""
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parent
MANAGEMENT = {'heartbeat_interval_seconds': 5, 'telemetry_flush_interval_ms': 500}


def plan(base):
    return [
        {'key': 'modbus', 'type_id': 'conpot', 'title': 'Conpot — промышленный Modbus', 'port': base},
        {'key': 's7comm', 'type_id': 'conpot', 'title': 'Conpot — контроллер Siemens S7', 'port': base + 1},
        {'key': 'http', 'type_id': 'dionaea', 'title': 'Dionaea — HTTP-сервер', 'port': base + 2},
        {'key': 'memcache', 'type_id': 'dionaea', 'title': 'Dionaea — Memcached', 'port': base + 3},
        {'key': 'mail', 'type_id': 'tcp-banner', 'title': 'TCP — почтовый сервер', 'port': base + 4},
        {'key': 'console', 'type_id': 'tcp-banner', 'title': 'TCP — служебная консоль', 'port': base + 5},
        {'key': 'redis', 'type_id': 'redis-emulator', 'title': 'Redis — база данных-приманка', 'port': base + 6},
        {'key': 'ssh', 'type_id': 'ssh-high-interaction', 'title': 'SSH — Linux-сервер', 'port': base + 7},
    ]


def config(item, decoy_password):
    kind, key, port = item['type_id'], item['key'], item['port']
    if kind == 'tcp-banner':
        banner = '220 mail.corp.local ESMTP ready\r\n' if key == 'mail' else 'Forge maintenance console ready\r\n'
        return {'listeners': [{'name': key, 'port': port, 'banner': banner, 'close_after_banner': False}],
                'logging': {'capture_payload': True, 'max_payload_bytes': 4096}, 'management': MANAGEMENT.copy()}
    service = {'name': key, 'port': port}
    result = {'services': [service], 'management': MANAGEMENT.copy()}
    if kind in ('conpot', 'dionaea'):
        service['protocol'] = key
        result['session'] = {'max_connections': 8, 'max_seconds': 60}
    elif kind == 'redis-emulator':
        service['password'] = decoy_password
    else:
        service.update(username='analyst', password=decoy_password)
        result['session'] = {'max_connections': 4, 'max_seconds': 120,
                             'max_recording_bytes': 65536, 'max_upload_bytes': 4096}
    return result


def boards(base):
    result = {}
    for role in ('portal', 'gateway'):
        board = json.loads((ROOT / 'boards' / (role + '.json')).read_text(encoding='utf-8'))
        offset = 10 if role == 'portal' else 12
        inputs = [n for n in board['nodes'] if n['type'] == 'http/input']
        for i, node in enumerate(inputs):
            node['config']['port'] = base + offset + i
        result[role] = board
    return result


def choose_ports(occupied, preferred):
    if not 1024 <= preferred <= 65516:
        raise ValueError('Начальный порт должен быть от 1024 до 65516')
    for base in range(preferred, 65517, 20):
        if not occupied.intersection(range(base, base + 20)):
            return base
    raise RuntimeError('Не найден свободный диапазон из 20 портов; задайте --port-base')
