"""HTTP behavior blocks compiled to the Python managed runtime contract."""
import json
import re
import uuid
from fastapi import HTTPException
from .models import Node
from .node_registry import field, port


def spec(kind, name, fields, outputs=('out',), doc=''):
    return {'type': 'http/' + kind, 'name': name, 'category': 'http', 'icon': 'git-branch',
            'subtitle': 'Блок HTTP-поведения', 'risk': 'low', 'config': fields,
            'ports': [port('in', 'данные', 'in'), *[port(p, {'yes': 'да', 'no': 'нет', 'error': 'ошибка'}.get(p, p), 'out') for p in outputs]],
            'doc': doc + ' Переменные: ${json.user}, ${vars.name}, ${query.id}, ${headers.authorization}, ${method}, ${path}. JSON-подстановка сохраняет тип значения.'}


HTTP_TYPES = [
    spec('input', 'HTTP-вход', [field('name', 'Имя сервиса', 'text', 'web'), field('port', 'Порт', 'number', 32080, min=1, max=65535), field('method', 'Метод', 'select', 'ANY', options=['ANY', 'GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS']), field('path', 'Путь (или *)', 'text', '*')], doc='Создаёт HTTP-вход. Вход in принимает данные другого блока внутри этой доски.'),
    spec('parse', 'Разобрать запрос', [field('format', 'Формат', 'select', 'json', options=['json', 'form', 'text', 'auto'])], ('out', 'error')),
    spec('extract', 'Извлечь поле', [field('source', 'Источник', 'text', 'json.user'), field('key', 'Переменная', 'text', 'user'), field('required', 'Поле обязательно', 'toggle', True)], ('out', 'error')),
    spec('condition', 'Условие', [field('source', 'Источник', 'text', 'json.action'), field('operator', 'Оператор', 'select', 'equals', options=['equals', 'not_equals', 'contains', 'exists', 'gt', 'lt', 'glob']), field('value', 'Значение', 'text', 'login')], ('yes', 'no', 'error'), 'glob: безопасная маска * и ?.'),
    spec('switch', 'Выбор ветки', [field('source', 'Источник', 'text', 'json.action'), *[field('value' + str(i), 'Вариант ' + str(i), 'text', str(i)) for i in range(1, 4)]], ('case1', 'case2', 'case3', 'default', 'error')),
    spec('transform', 'Преобразовать данные', [field('key', 'Переменная', 'text', 'data'), field('operation', 'Операция', 'select', 'assign', options=['assign', 'upper', 'lower', 'add', 'merge', 'remove']), field('value', 'Значение JSON / шаблон', 'textarea', '"${json}"')], ('out', 'error')),
    spec('state', 'Данные сессии', [field('operation', 'Операция', 'select', 'get', options=['get', 'set', 'delete']), field('key', 'Ключ', 'text', 'step'), field('value', 'Значение JSON / шаблон', 'textarea', '1')], ('out', 'error'), 'Сессия определяется cookie hf_session, срок хранения 30 минут.'),
    spec('call', 'Вызвать другую ловушку', [field('target', 'HTTP-вход получателя', 'node', ''), field('payload', 'Передаваемый JSON', 'textarea', '"${json}"'), field('key', 'Переменная результата', 'text', 'result')], ('out', 'error'), 'Выберите HTTP-вход на этой доске. Результат доступен в vars.result, статус — vars.result_status.'),
    spec('send', 'Передать в отдельную ловушку', [field('target_id', 'Ловушка-получатель', 'trap', ''), field('service', 'Имя HTTP-входа получателя', 'text', 'web'), field('method', 'Метод', 'select', 'POST', options=['POST', 'PUT', 'PATCH']), field('path', 'Эндпоинт получателя', 'text', '/pipeline'), field('payload', 'Передаваемый JSON', 'textarea', '"${json}"')], ('out', 'error'), 'Передаёт JSON в отдельно развёрнутую HTTP-ловушку вашей организации. out означает принятие в очередь агента; обработка продолжается у получателя. На получателе настройте HTTP-вход с этим именем, методом и путём. Ошибки связи идут в error. Развёртывание создаёт разрешённую связь.'),
    spec('fork', 'Параллельные ветки', [], ('branch1', 'branch2', 'branch3', 'out', 'error'), 'Каждая ветка имеет свою копию данных и должна закончиться ответом. out продолжается после завершения всех веток.'),
    spec('join', 'Собрать результаты', [field('key', 'Переменная результатов', 'text', 'results')], ('out', 'error'), 'Собирает результаты предшествующего разветвления по именам branch1, branch2, branch3.'),
    spec('event', 'Отправить событие', [field('name', 'Имя события', 'text', 'pipeline.stage'), field('severity', 'Важность телеметрии', 'select', 'medium', options=['low', 'medium', 'high', 'critical'], hint='low — обычное (белый); medium — средняя важность (жёлтый); high — высокая (красный); critical — критическая (багровый). Цвет применяется в журнале и уведомлениях.'), field('payload', 'Данные события (JSON / шаблон)', 'textarea', '"${json}"')], ('out', 'error'), 'Поставьте блок на нужном этапе или после условия, чтобы отметить важную телеметрию. Выберите важность события в настройках блока. Отправляет http.custom с вашим именем и JSON; технический журнал сохраняется. Данные события — до 4 КБ.'),
    spec('delay', 'Задержка', [field('milliseconds', 'Миллисекунды', 'number', 250, min=0, max=5000)], ('out', 'error')),
    spec('response', 'HTTP-ответ', [field('format', 'Формат', 'select', 'json', options=['json', 'text', 'html']), field('status', 'HTTP-статус', 'number', 200, min=200, max=599), field('body', 'Тело / шаблон', 'textarea', '{"status":"ok"}'), field('headers', 'Заголовки и Set-Cookie (JSON)', 'textarea', '{}')], (), 'Формирует ответ клиенту либо возвращает результат вызывающему блоку.'),
    spec('redirect', 'Перенаправить', [field('status', 'Статус', 'select', '302', options=['301', '302', '303', '307', '308']), field('url', 'Адрес / шаблон', 'text', '/login')], ()),
    spec('error', 'Обработать ошибку', [field('key', 'Переменная ошибки', 'text', 'error')], ('out',), 'Подключите к выходу error. Сообщение доступно в переменной и может войти в HTTP-ответ.'),
]


def compile_http(chain):
    nodes = {n.id: n for n in chain.nodes if n.type.startswith('http/')}
    if not nodes:
        return None
    specs = {s['type']: s for s in HTTP_TYPES}
    if len(nodes) > 512:
        raise HTTPException(422, 'Максимум 512 HTTP-блоков на доске')
    blocks, services = [], []
    for node in nodes.values():
        if node.type not in specs:
            raise HTTPException(422, 'Неизвестный HTTP-блок')
        config = {f['key']: node.config.get(f['key'], f['default']) for f in specs[node.type]['config']}
        for f in specs[node.type]['config']:
            value = config[f['key']]
            if f['type'] == 'number' and (type(value) is not int or not f['min'] <= value <= f['max']):
                raise HTTPException(422, f"{node.id}: неверное значение {f['label']}")
            if f['type'] == 'toggle' and type(value) is not bool:
                raise HTTPException(422, f'{node.id}: ожидается переключатель')
            if f['type'] not in ('number', 'toggle') and (not isinstance(value, str) or len(value.encode()) > 8192):
                raise HTTPException(422, f'{node.id}: неверное строковое значение')
            if f.get('options') and value not in f['options']:
                raise HTTPException(422, f'{node.id}: неизвестный вариант')
        if 'key' in config and not re.fullmatch(r'[a-zA-Z][a-zA-Z0-9_]{0,31}', config['key']):
            raise HTTPException(422, f'{node.id}: неверное имя переменной')
        if node.type == 'http/input':
            if not re.fullmatch(r'[a-z][a-z0-9_-]{0,31}', config['name']) or not (config['path'] == '*' or config['path'].startswith('/')):
                raise HTTPException(422, f'{node.id}: неверное имя сервиса или путь')
            services.append({'name': config['name'], 'port': config['port'], 'entry': node.id})
        if node.type == 'http/call' and (config['target'] not in nodes or nodes[config['target']].type != 'http/input'):
            raise HTTPException(422, f'{node.id}: выберите HTTP-вход получателя')
        if node.type == 'http/event' and not re.fullmatch(r'[a-z][a-z0-9_.-]{0,63}', config['name']):
            raise HTTPException(422, f'{node.id}: неверное имя события')
        if node.type == 'http/send':
            try:
                config['target_id']=str(uuid.UUID(config['target_id']))
            except (ValueError,AttributeError) as error:
                raise HTTPException(422,f'{node.id}: выберите ловушку-получатель') from error
            if not re.fullmatch(r'[a-z][a-z0-9_-]{0,31}',config['service']) or not config['path'].startswith('/') or config['path'].startswith('//') or any(ch in config['path'] for ch in ('?', '#', '\r', '\n')):
                raise HTTPException(422,f'{node.id}: неверный HTTP-вход или эндпоинт')
        json_fields = []
        if node.type in ('http/transform', 'http/state'):
            json_fields.append('value')
        if node.type in ('http/call', 'http/event', 'http/send'):
            json_fields.append('payload')
        if node.type == 'http/response':
            json_fields.append('headers')
            if config['format'] == 'json':
                json_fields.append('body')
        for key in json_fields:
            try:
                def invalid_constant(value):
                    raise ValueError('Неконечное число JSON: ' + value)
                parsed = json.loads(config[key], parse_constant=invalid_constant)
            except ValueError as error:
                raise HTTPException(422, f'{node.id}: неверный JSON в {key}') from error
            if key == 'headers' and (not isinstance(parsed, dict) or any(not isinstance(v, str) or '\r' in v or '\n' in v or not re.fullmatch(r'[!#$%&\x27*+.^_`|~0-9a-zA-Z-]+', k) or k.lower() in ('content-length', 'transfer-encoding', 'connection') for k, v in parsed.items())):
                raise HTTPException(422, f'{node.id}: неверные заголовки')
        blocks.append({'id': node.id, 'kind': node.type[5:], 'config': config, 'outputs': {}})
    by_id = {b['id']: b for b in blocks}
    for edge in chain.edges:
        if edge.source not in nodes and edge.target not in nodes:
            continue
        if edge.source not in nodes or edge.target not in nodes or edge.target_port != 'in':
            raise HTTPException(422, 'HTTP-блоки соединяются с HTTP-блоками через вход in')
        allowed = {p['id'] for p in specs[nodes[edge.source].type]['ports'] if p['kind'] == 'out'}
        outputs = by_id[edge.source]['outputs']
        if edge.source_port not in allowed or edge.source_port in outputs:
            raise HTTPException(422, 'Каждый выход блока должен иметь одну связь')
        outputs[edge.source_port] = edge.target
    for b in blocks:
        required = {'yes', 'no'} if b['kind'] == 'condition' else {'case1', 'case2', 'case3', 'default'} if b['kind'] == 'switch' else set() if b['kind'] in ('response', 'redirect') else {'out'}
        if not required.issubset(b['outputs']) or b['kind'] == 'fork' and not any(k.startswith('branch') for k in b['outputs']):
            raise HTTPException(422, f"{b['id']}: соедините обязательные выходы")
    if not services or len(services) > 32 or len({s['name'] for s in services}) != len(services) or len({s['port'] for s in services}) != len(services):
        raise HTTPException(422, 'Добавьте HTTP-входы с разными именами и портами')
    reached, pending = set(), [s['entry'] for s in services]
    while pending:
        key = pending.pop()
        if key in reached:
            continue
        reached.add(key)
        pending.extend(by_id[key]['outputs'].values())
        if by_id[key]['kind'] == 'call':
            pending.append(by_id[key]['config']['target'])
    if len(reached) != len(blocks):
        raise HTTPException(422, 'Есть HTTP-блоки, недостижимые от входов')
    config = {
        'services': services, 'blocks': blocks, 'limits': {'max_steps': 512, 'timeout_ms': 10000, 'max_body_bytes': 65536},
        'management': {'heartbeat_interval_seconds': 10, 'telemetry_flush_interval_ms': 500}}
    if len(json.dumps(config, ensure_ascii=False, allow_nan=False).encode()) > 128 * 1024:
        raise HTTPException(422, 'Конфигурация превышает 128 КБ; уменьшите шаблоны ответов')
    return Node(id='__http__', type='build/custom-http', label=chain.name + ' HTTP', config={'compiled': config})
