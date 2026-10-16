"""Interactive HoneyForge jury demo. Every action requires Enter."""
import argparse
from datetime import datetime, timezone
import importlib.util
from pathlib import Path
import sys
import uuid
import webbrowser

from api import ApiError, Client, origin, wait_for
from events import Dashboard, custom_event, protocol_events, rows
from probes import http_probe, probe, require
from scenarios import ROOT, boards, choose_ports, plan
from stand import Stand

DEFAULT_SITE = 'https://honey-forge-seg-fault.ru'


def pause(title, explanation=''):
    print('\n' + title, flush=True)
    if explanation:
        print(explanation, flush=True)
    input('Enter — выполнить; Ctrl+C — остановить: ')


def preflight(client, preferred):
    entries = list(client.items('/api/trap-types'))
    catalog = {}
    for entry in entries:
        if entry.get('available_for_new_profiles'):
            old = catalog.get(entry['type_id'])
            if old is None or entry['type_version'] > old['type_version']:
                catalog[entry['type_id']] = entry
    required = {i['type_id'] for i in plan(preferred)} | {'custom-http'}
    require(required <= catalog.keys(), 'На сервере недоступны типы: ' + ', '.join(sorted(required - catalog.keys())))
    client.request('GET', '/constructor/api/chains')
    occupied = set()
    for profile in client.items('/api/profiles'):
        cfg = profile.get('config', {})
        for listener in cfg.get('services', cfg.get('listeners', [])):
            occupied.add(listener['port'])
    # Drafts also reserve their ports for this demo's preflight.
    for summary in client.request('GET', '/constructor/api/chains'):
        board = client.request('GET', '/constructor/api/chains/' + summary['id'])
        occupied.update(n['config']['port'] for n in board['nodes']
                        if n['type'] in ('http/input', 'custom/listener'))
    base = choose_ports(occupied, preferred)
    print(f'Проверка пройдена. Демо-порты: {base}–{base + 13}.')
    return catalog, base


def listener_ready(host, port):
    import socket
    def check():
        try:
            with socket.create_connection((host, port), timeout=2):
                return True
        except OSError:
            return False
    wait_for(check, f'Доступ к {host}:{port}; проверьте публикацию портов/firewall', 45)


def protocol_step(stand, item):
    trap = stand.data['traps'][item['key']]
    host = stand.data['host']
    listener_ready(host, trap['port'])
    marker = 'jury-' + uuid.uuid4().hex[:12]
    with Dashboard(stand.client) as dashboard:
        before = {event['event_id'] for event in rows(stand.client, trap['id'])}
        source = probe(item, host, stand.password, marker)
        events = protocol_events(stand.client, trap['id'], source, item['key'], before=before)
        dashboard.delivered([e['event_id'] for e in events])
    stand.record(item['title'], events)


def pipeline_step(stand, score):
    traps, client = stand.data['traps'], stand.client
    marker = 'jury-pipeline-' + uuid.uuid4().hex
    with Dashboard(client) as dashboard:
        status, raw, _ = http_probe(stand.data['host'], traps['portal']['port'], '/identity',
                                    {'action': 'login', 'user': 'Alice', 'score': score, 'run': marker})
        import json
        reply = json.loads(raw)
        require(status == 202 and reply.get('queued') is True, 'Портал не принял передачу JSON')
        require(set(reply.get('results', {})) == {'branch1', 'branch2', 'branch3'}, 'Не выполнены три ветки fork/join')
        first = custom_event(client, traps['portal']['id'], marker, 'jury.identity.login')
        name = 'jury.gateway.high_risk' if score > 70 else 'jury.gateway.backup'
        second = custom_event(client, traps['gateway']['id'], marker, name)
        payload = second['data']['payload']
        require(payload.get('user') == 'alice' and payload.get('score') == score and payload.get('action') == 'backup',
                'Шлюз не сохранил преобразованные данные первой ловушки')
        require(second['data']['severity'] == ('critical' if score > 70 else 'high'), 'Неверная важность события шлюза')
        dashboard.delivered([first['event_id'], second['event_id']])
    print(f'  Alice → alice; score={score} → {name}; run={marker}')
    print('  Портал HTTP 202:', raw.decode()[:1200])
    stand.record('Зависимость двух ловушек, score=' + str(score), [first, second])


def direct_gateway(stand):
    marker = 'jury-gateway-' + uuid.uuid4().hex
    trap = stand.data['traps']['gateway']
    with Dashboard(stand.client) as dashboard:
        status, raw, _ = http_probe(stand.data['host'], trap['port'], '/gateway',
                                    {'action': 'status', 'user': 'Jury', 'score': 10, 'run': marker})
        require(status == 200, 'Прямой запрос к шлюзу не вернул 200')
        event = custom_event(stand.client, trap['id'], marker, 'jury.gateway.health')
        dashboard.delivered([event['event_id']])
    print('  Шлюз HTTP 200:', raw.decode()[:1200])
    stand.record('Прямое подключение к шлюзу и внутренний health-вызов', [event])


def invalid_step(stand):
    trap = stand.data['traps']['portal']
    for body in (b'{', b'{"action":"unknown","user":"Jury"}'):
        status, raw, _ = http_probe(stand.data['host'], trap['port'], '/identity', raw=body)
        require(status == 422, f'Невалидный запрос: ожидался 422, получен {status}')
        print('  HTTP', status, raw.decode()[:300])
    stand.data['checks'].append({'name': 'Невалидный JSON и неизвестная операция отклонены', 'passed': True})
    stand.save()


def run():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--site', help='HTTPS-адрес сайта; иначе запрос при запуске')
    parser.add_argument('--host', help='Адрес опубликованных портов ловушек; по умолчанию адрес сайта')
    parser.add_argument('--ca', help='Доверенный CA/сертификат для другого сайта')
    parser.add_argument('--port-base', type=int, default=34000)
    parser.add_argument('--preflight', action='store_true', help='Войти и проверить API без создания ресурсов')
    parser.add_argument('--no-browser', action='store_true')
    args = parser.parse_args()
    for module in ('websocket', 'paramiko'):
        require(importlib.util.find_spec(module) is not None, 'Установите зависимости: python -m pip install -r requirements.txt')
    site = origin(args.site or input(f'Site [{DEFAULT_SITE}]: ').strip() or DEFAULT_SITE)
    ca = args.ca or (str(ROOT / 'server-certificate.pem') if site == DEFAULT_SITE else None)
    client = Client(site, ca)
    client.login()
    catalog, base = preflight(client, args.port_base)
    host = args.host or client.url.hostname
    if args.preflight:
        with Dashboard(client):
            print('Фронтенд WebSocket: stream.ready. Ресурсы не создавались.')
        return
    output = ROOT / 'runs' / (datetime.now(timezone.utc).strftime('%Y%m%d-%H%M%S') + '-' + uuid.uuid4().hex[:6] + '.json')
    stand = Stand(client, catalog, base, output, host)
    print('Отчёт и ID созданных ресурсов:', output)
    print('Создадим 10 новых ловушек. В конце они останутся доступны в панели.')
    pause('1. Откройте экран для жюри', 'Войдите в браузере в ту же организацию. Откройте Конструктор сети и журнал событий.')
    if not args.no_browser:
        webbrowser.open(site + '/app/network')
    try:
        with Dashboard(client):
            print('Канал фронтенда готов.')
        for i, item in enumerate(plan(base), 2):
            pause(f'{i}. Создать {item["title"]}', f'Появится новая ловушка; порт {item["port"]}.')
            stand.create_trap(item)
        for role in ('gateway', 'portal'):
            board = boards(base)[role]
            pause('Создать схему: ' + role, f'{len(board["nodes"])} блоков; {len(board["edges"])} соединений. Посмотрите её в конструкторе ловушек.')
            stand.create_board(role, board)
        pause('Соединить Портал → Шлюз', 'Стрелка разрешит передачу JSON между двумя новыми HTTP-ловушками.')
        stand.connect()
        for item in plan(base):
            pause('Подключиться: ' + item['title'], 'Смотрите ответы в терминале и новые события в журнале сайта.')
            protocol_step(stand, item)
        pause('Прямой запрос к HTTP-шлюзу', 'Шлюз вызовет собственный внутренний health-сервис.')
        direct_gateway(stand)
        for score in (25, 90):
            pause(f'Связанные ловушки: score={score}', 'Один запрос в Портал → три ветки → нормализация имени → JSON в Шлюз → выбор реакции по риску.')
            pipeline_step(stand, score)
        pause('Ошибочные запросы', 'Проверим сломанный JSON и неизвестную операцию: HTTP 422.')
        invalid_step(stand)
        stand.data['passed'] = True
        stand.save()
        print('\nДемонстрация завершена. 10 ловушек остаются в панели. Все проверки пройдены.')
    except BaseException:
        stand.save()
        print('\nДемонстрация остановлена. Созданные ресурсы сохранены; их ID — в отчёте.')
        raise
    finally:
        print('Отчёт:', output, flush=True)


if __name__ == '__main__':
    sys.stdout.reconfigure(encoding='utf-8')
    sys.stderr.reconfigure(encoding='utf-8')
    try:
        run()
    except KeyboardInterrupt:
        print('\nОстановлено пользователем.')
        sys.exit(130)
    except Exception as error:
        print(f'ОШИБКА: {type(error).__name__}: {error}', file=sys.stderr)
        sys.exit(1)
