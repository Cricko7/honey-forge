"""Three readable boards for real HTTP pipeline acceptance and demos."""


def node(node_id, kind, x, y, label, **config):
    return {'id': node_id, 'type': 'http/' + kind, 'label': label,
            'position': {'x': x, 'y': y}, 'config': config}


def edges(items):
    return [{'source': a, 'source_port': port, 'target': b, 'target_port': 'in'}
            for a, port, b in items]


def reply(key, x, y, status=200, body='{}'):
    return node(key, 'response', x, y, 'Ответ ' + str(status),
                status=status, format='json', body=body, headers='{}')


def event(key, x, y, name, severity='medium', payload='"${json}"'):
    return node(key, 'event', x, y, name, name=name, severity=severity, payload=payload)


def collector(port):
    nodes = [
        node('web', 'input', 0, 180, 'C: приём результата', name='web', port=port, method='POST', path='/collect'),
        node('parse', 'parse', 280, 180, 'Разобрать JSON', format='json'),
        node('extract', 'extract', 560, 180, 'Обязательный пользователь', source='json.user', key='user', required=True),
        node('upper', 'transform', 840, 180, 'Нормализовать имя', key='user', operation='upper', value='"${vars.user}"'),
        node('state', 'state', 1120, 180, 'Сохранить результат сессии', operation='set', key='result', value='{"user":"${vars.user}","run":"${json.run}"}'),
        node('risk', 'condition', 1400, 180, 'Оценка выше 50?', source='json.score', operator='gt', value='50'),
        event('alert', 1680, 30, 'collector.alert', 'high', '{"user":"${vars.user}","run":"${json.run}","score":"${json.score}"}'),
        event('observe', 1680, 350, 'collector.observe', 'low'),
        reply('done', 1960, 180, body='{"stored":true,"user":"${vars.user}"}'),
        reply('invalid', 840, 500, 422, '{"error":"invalid payload"}'),
    ]

    return nodes, edges([
        ('web', 'out', 'parse'), ('parse', 'out', 'extract'), ('parse', 'error', 'invalid'),
        ('extract', 'out', 'upper'), ('extract', 'error', 'invalid'), ('upper', 'out', 'state'),
        ('state', 'out', 'risk'), ('risk', 'yes', 'alert'), ('risk', 'no', 'observe'),
        ('risk', 'error', 'invalid'), ('alert', 'out', 'done'), ('observe', 'out', 'done'),
    ])


def processor(port, target):
    nodes = [
        node('web', 'input', 0, 180, 'B: обработка', name='web', port=port, method='POST', path='/ingest'),
        node('parse', 'parse', 280, 180, 'Разобрать JSON', format='json'),
        node('upper', 'transform', 560, 180, 'Преобразовать пользователя', key='user', operation='upper', value='"${json.user}"'),
        node('risk', 'condition', 840, 180, 'Высокий риск?', source='json.score', operator='gt', value='50'),
        event('high', 1120, 20, 'processor.high', 'high'),
        event('low', 1120, 360, 'processor.low', 'low'),
        node('send', 'send', 1400, 180, 'Передать в C', target_id=target, service='web', method='POST', path='/collect', payload='{"user":"${vars.user}","score":"${json.score}","run":"${json.run}"}'),
        reply('accepted', 1680, 180, 202, '{"forwarded":true}'),
        event('failure', 1400, 520, 'processor.delivery_error', 'high'),
        reply('unavailable', 1680, 520, 503, '{"error":"handoff rejected"}'),
        reply('invalid', 560, 520, 422, '{"error":"invalid input"}'),
    ]

    return nodes, edges([
        ('web', 'out', 'parse'), ('parse', 'out', 'upper'), ('parse', 'error', 'invalid'),
        ('upper', 'out', 'risk'), ('risk', 'yes', 'high'), ('risk', 'no', 'low'),
        ('risk', 'error', 'invalid'), ('high', 'out', 'send'), ('low', 'out', 'send'),
        ('send', 'out', 'accepted'), ('send', 'error', 'failure'), ('failure', 'out', 'unavailable'),
    ])


def coordinator(port, internal_port, target):
    nodes = [
        node('web', 'input', 0, 220, 'A: POST /login', name='web', port=port, method='POST', path='/login'),
        node('parse', 'parse', 280, 220, 'JSON', format='json'),
        node('action', 'condition', 560, 220, 'action = login?', source='json.action', operator='equals', value='login'),
        node('fork', 'fork', 840, 220, 'Три параллельные ветки'),
        event('login', 1120, 0, 'login.accepted', 'high'),
        node('send', 'send', 1400, 0, 'Передать в B', target_id=target, service='web', method='POST', path='/ingest', payload='{"user":"${json.user}","score":"${json.score}","run":"${json.run}"}'),
        reply('sent', 1680, 0, 202, '{"queued":true}'),
        node('state', 'state', 1120, 300, 'Запомнить попытку', operation='set', key='attempt', value='"${json.run}"'),
        node('delay', 'delay', 1400, 300, 'Задержка аудита', milliseconds=250),
        event('audit', 1680, 300, 'audit.received'),
        reply('audited', 1960, 300, body='{"audit":true}'),
        node('call', 'call', 1120, 620, 'Внутренний вызов /lookup', target='lookup', key='lookup', payload='{"user":"${json.user}"}'),
        reply('looked', 1400, 620, body='"${vars.lookup}"'),
        node('join', 'join', 2240, 220, 'Собрать три результата', key='results'),
        reply('done', 2520, 220, 202, '{"results":"${vars.results}"}'),
        node('lookup', 'input', 1120, 930, 'Внутренний HTTP-вход', name='lookup', port=internal_port, method='ANY', path='/lookup'),
        node('upper', 'transform', 1400, 930, 'Имя в верхнем регистре', key='user', operation='upper', value='"${json.user}"'),
        reply('lookup_done', 1680, 930, body='{"user":"${vars.user}"}'),
        reply('bad_json', 280, 600, 422, '{"error":"invalid JSON"}'),
        reply('denied', 560, 600, 422, '{"error":"unsupported action"}'),
        event('failure', 1400, -310, 'coordinator.delivery_error', 'high'),
        reply('unavailable', 1680, -310, 503, '{"error":"handoff rejected"}'),
    ]

    return nodes, edges([
        ('web', 'out', 'parse'), ('parse', 'out', 'action'), ('parse', 'error', 'bad_json'),
        ('action', 'yes', 'fork'), ('action', 'no', 'denied'),
        ('fork', 'branch1', 'login'), ('login', 'out', 'send'), ('send', 'out', 'sent'),
        ('send', 'error', 'failure'), ('failure', 'out', 'unavailable'),
        ('fork', 'branch2', 'state'), ('state', 'out', 'delay'), ('delay', 'out', 'audit'), ('audit', 'out', 'audited'),
        ('fork', 'branch3', 'call'), ('call', 'out', 'looked'),
        ('fork', 'out', 'join'), ('fork', 'error', 'unavailable'), ('join', 'out', 'done'),
        ('lookup', 'out', 'upper'), ('upper', 'out', 'lookup_done'),
    ])
