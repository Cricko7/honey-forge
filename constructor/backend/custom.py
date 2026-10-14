"""Compile connected behavior blocks into one multi-port custom TCP trap."""
from fastapi import HTTPException
from .models import Node
from .node_registry import field, port

CUSTOM_TYPES = [
    {'type': 'custom/listener', 'name': 'TCP-вход', 'icon': 'server', 'config': [field('name', 'Имя сервиса', 'text', 'service'), field('port', 'TCP-порт', 'number', 32000, min=1, max=65535)]},
    {'type': 'custom/banner', 'name': 'Приветствие', 'icon': 'file', 'config': [field('text', 'Ответ при подключении', 'textarea', '220 Welcome\r\n'), field('close', 'Закрыть после приветствия', 'toggle', False)]},
    {'type': 'custom/rule', 'name': 'Запрос → ответ', 'icon': 'terminal', 'config': [field('match', 'Условие', 'select', 'exact', options=['exact', 'prefix', 'contains', 'any']), field('input', 'Входная строка', 'text', 'PING'), field('response', 'Ответ', 'textarea', 'PONG\r\n'), field('close', 'Закрыть после ответа', 'toggle', False)]},
]


def registry_types():
    return [{**spec, 'category': 'custom', 'subtitle': 'Блок новой ловушки', 'risk': 'low',
             'ports': [port('in', 'предыдущий блок', 'in'), port('out', 'следующий блок', 'out')],
             'doc': 'Соедините TCP-вход, приветствие и правила в порядке проверки. Запросы завершаются переводом строки.'} for spec in CUSTOM_TYPES]


def compile_chain(chain):
    blocks = {node.id: node for node in chain.nodes if node.type.startswith('custom/')}
    if not blocks:
        return None
    if any(node.type not in {s['type'] for s in CUSTOM_TYPES} for node in blocks.values()):
        raise HTTPException(422, 'Неизвестный блок поведения')
    listeners = []
    reached = set()
    outgoing = {}
    for edge in chain.edges:
        if edge.source not in blocks:
            continue
        if edge.target not in blocks or edge.source_port != 'out' or edge.target_port != 'in':
            raise HTTPException(422, 'Блоки поведения нужно соединять выходом out со входом in')
        outgoing.setdefault(edge.source, []).append(edge.target)
    for node in blocks.values():
        if node.type != 'custom/listener':
            continue
        listener = {'name': node.config.get('name', 'service'), 'port': node.config.get('port'), 'banner': '', 'close_after_banner': False, 'rules': []}
        if type(listener['port']) is not int or not 1 <= listener['port'] <= 65535:
            raise HTTPException(422, 'TCP-порт должен быть целым числом от 1 до 65535')
        current = node
        while current:
            if current.id in reached:
                raise HTTPException(422, 'Циклы и общие блоки поведения не поддерживаются')
            reached.add(current.id)
            if current.type == 'custom/banner':
                if listener['banner'] or listener['rules']:
                    raise HTTPException(422, 'Приветствие должно быть первым блоком после TCP-входа')
                listener['banner'] = current.config.get('text', '')
                listener['close_after_banner'] = current.config.get('close', False)
            elif current.type == 'custom/rule':
                listener['rules'].append({key: current.config.get(key, default) for key, default in [('match', 'exact'), ('input', ''), ('response', ''), ('close', False)]})
            elif current is not node:
                raise HTTPException(422, 'TCP-вход не может быть частью другого TCP-входа')
            targets = outgoing.get(current.id, [])
            if len(targets) > 1:
                raise HTTPException(422, 'Соедините правила последовательно в порядке проверки')
            current = blocks[targets[0]] if targets else None
        if listener['close_after_banner'] and listener['rules']:
            raise HTTPException(422, 'Правила недостижимы при закрытии после приветствия')
        listeners.append(listener)
    if not listeners or len(reached) != len(blocks):
        raise HTTPException(422, 'Каждый блок поведения должен быть соединён с TCP-входом')
    if len({l['port'] for l in listeners}) != len(listeners) or len({l['name'] for l in listeners}) != len(listeners):
        raise HTTPException(422, 'TCP-входы должны иметь разные имена и порты')
    config = {'listeners': listeners, 'logging': {'capture_payload': True, 'max_payload_bytes': 4096},
              'management': {'heartbeat_interval_seconds': 10, 'telemetry_flush_interval_ms': 500}}
    return Node(id='__custom__', type='build/custom-tcp', label=chain.name, config={'compiled': config})
