"""Create only this run's resources and record IDs immediately after each write."""
from datetime import datetime, timezone
import json
import secrets
import uuid

from api import wait_for
from probes import require
from scenarios import config


class Stand:
    def __init__(self, client, catalog, base, output, host):
        self.client, self.catalog, self.output = client, catalog, output
        self.password = secrets.token_urlsafe(24)
        self.data = {'base_url': client.base, 'host': host, 'port_base': base,
                     'run': datetime.now(timezone.utc).strftime('%Y%m%d-%H%M%S') + '-' + uuid.uuid4().hex[:5],
                     'profiles': [], 'traps': {}, 'chains': {}, 'links': [], 'checks': [], 'passed': False}
        self.save()

    def save(self):
        self.output.parent.mkdir(parents=True, exist_ok=True)
        tmp = self.output.with_suffix('.tmp')
        tmp.write_text(json.dumps(self.data, ensure_ascii=False, indent=2), encoding='utf-8')
        tmp.replace(self.output)

    def running(self, trap_id):
        latest = {}
        def check():
            trap = self.client.request('GET', '/api/traps/' + trap_id)
            latest.update(trap)
            if trap['runtime_state'] == 'error':
                raise RuntimeError('Ошибка запуска ловушки ' + trap_id + '; смотрите состояние в панели')
            if trap['runtime_state'] == 'running' and trap['connectivity'] == 'online':
                return trap
            return None
        try:
            return wait_for(check, 'Запуск ловушки ' + trap_id, 240)
        except TimeoutError as error:
            detail = 'Агент ещё ни разу не подключался' if latest.get('last_seen_at') is None else 'Агент не перешёл в running/online'
            raise TimeoutError(detail + ': ' + trap_id + '. Проверьте Docker-задачу и логи агента на сервере.') from error

    def create_trap(self, item):
        name = 'Жюри ' + self.data['run'] + ' — ' + item['title']
        entry = self.catalog[item['type_id']]
        profile = self.client.request('POST', '/api/profiles', {
            'request_id': str(uuid.uuid4()), 'name': name, 'type_id': item['type_id'],
            'type_version': entry['type_version'], 'config': config(item, self.password)})
        self.data['profiles'].append(profile['id'])
        self.save()
        trap = self.client.request('POST', '/api/traps', {
            'request_id': str(uuid.uuid4()), 'name': name, 'profile_id': profile['id'],
            'description': 'Живая демонстрация для жюри. Только вымышленные данные.'})
        self.data['traps'][item['key']] = {'id': trap['id'], 'port': item['port'], 'name': name}
        self.save()
        self.running(trap['id'])
        print('  Работает:', name, f'(:{item["port"]})', flush=True)

    def create_board(self, role, board):
        title = 'Портал: три ветки и передача JSON' if role == 'portal' else 'Шлюз: риск, аудит и внутренний сервис'
        chain = self.client.request('POST', '/constructor/api/chains', {
            'name': 'Жюри ' + self.data['run'] + ' — ' + title,
            'description': 'Портал → Шлюз. score=25: backup; score=90: high_risk. '
                           'Связь задаётся стрелкой в конструкторе сети. Данные вымышленные.',
            'tags': ['jury', 'live-demo']})
        self.data['chains'][role] = chain['id']
        self.save()
        self.client.request('PATCH', '/constructor/api/chains/' + chain['id'], board)
        saved = self.client.request('GET', '/constructor/api/chains/' + chain['id'])
        require(len(saved['nodes']) == len(board['nodes']) and len(saved['edges']) == len(board['edges']),
                'Сервер сохранил неполную схему')
        self.client.request('POST', '/constructor/api/chains/' + chain['id'] + '/deploy', {'action': 'deploy'})
        def bound():
            status = self.client.request('GET', '/constructor/api/chains/' + chain['id'] + '/runtime')
            traps = {t['id']: t for t in status.get('traps', {}).values()}
            require(len(traps) <= 1, 'Ожидалась одна ловушка на схему')
            return next(iter(traps.values()), None)
        trap = wait_for(bound, 'Создание HTTP-ловушки', 90)
        inputs = [n for n in board['nodes'] if n['type'] == 'http/input']
        self.data['traps'][role] = {'id': trap['id'], 'port': inputs[0]['config']['port'], 'name': title}
        if trap['profile_id'] not in self.data['profiles']:
            self.data['profiles'].append(trap['profile_id'])
        self.save()
        self.running(trap['id'])
        print(f'  Работает: {title}; {len(board["nodes"])} блоков, {len(board["edges"])} соединений.')
        print('  Конструктор:', self.client.base + '/app/constructor', '| выберите:', title)

    def connect(self):
        source, target = (self.data['traps'][r]['id'] for r in ('portal', 'gateway'))
        self.client.request('POST', '/api/trap-links', {'source_id': source, 'target_id': target})
        self.data['links'].append({'source_id': source, 'target_id': target})
        self.save()
        links = self.client.request('GET', '/api/trap-links')['items']
        require(any(x['source_id'] == source and x['target_id'] == target for x in links), 'Стрелка не сохранилась')
        print('  Портал → Шлюз: передача JSON включена. Откройте Конструктор сети.')

    def record(self, name, events):
        self.data['checks'].append({'name': name, 'passed': True,
                                    'event_ids': [e['event_id'] for e in events], 'frontend_ws': True})
        self.save()
        for event in events:
            print('  Событие:', event['event_type'], event['event_id'])
