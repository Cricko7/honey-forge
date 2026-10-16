"""Prove both persistence and delivery over the dashboard's actual WS protocol."""
import json
import socket
import time
import uuid
from urllib.parse import urlencode

from api import wait_for
from probes import require


class Dashboard:
    def __init__(self, client):
        self.client, self.socket = client, None

    def __enter__(self):
        import websocket
        url = 'wss://' + self.client.url.netloc + '/api/stream'
        # stdlib tries IPv4 too when a Windows IPv6 attempt returns EACCES.
        raw = socket.create_connection((self.client.url.hostname, self.client.url.port or 443), timeout=20)
        try:
            tls = self.client.context.wrap_socket(raw, server_hostname=self.client.url.hostname)
        except BaseException:
            raw.close()
            raise
        try:
            self.socket = websocket.create_connection(
                url, socket=tls, timeout=20, origin=self.client.base, cookie=self.client.cookie_header(),
                subprotocols=['dashboard-stream.v1'])
            self.socket.send(json.dumps({'message_id': str(uuid.uuid4()), 'type': 'stream.subscribe',
                                         'payload': {'after': None}}))
            deadline = time.monotonic() + 25
            while time.monotonic() < deadline:
                envelope = self.read()
                if envelope.get('type') == 'stream.ready':
                    return self
            raise TimeoutError('Фронтенд-канал не прислал stream.ready')
        except BaseException:
            if self.socket:
                self.socket.close()
            else:
                tls.close()
            raise

    def read(self):
        raw = self.socket.recv()
        require(bool(raw), 'Фронтенд-канал закрыл соединение')
        envelope = json.loads(raw)
        require(envelope.get('type') != 'error', 'Фронтенд-канал вернул ошибку протокола')
        return envelope

    def delivered(self, ids):
        pending = set(ids)
        deadline = time.monotonic() + 45
        while pending and time.monotonic() < deadline:
            envelope = self.read()
            if envelope.get('type') == 'event.created':
                event = envelope.get('payload', {}).get('data', {}).get('event', {})
                pending.discard(event.get('event_id'))
        require(not pending, 'События сохранены, но не получены по каналу фронтенда')
        print(f'  Фронтенд WebSocket: получены все {len(ids)} проверяемых событий.')

    def __exit__(self, *_):
        self.socket.close()


def rows(client, trap_id, event_type=None):
    cursor = None
    for _ in range(10):
        query = {'trap_id': trap_id, 'limit': 100}
        if event_type:
            query['event_type'] = event_type
        if cursor:
            query['cursor'] = cursor
        page = client.request('GET', '/api/events?' + urlencode(query))
        yield from page['items']
        cursor = page.get('next_cursor')
        if not cursor:
            return
    raise RuntimeError('Слишком много событий для одного шага демонстрации')


def protocol_events(client, trap_id, source_port, key, before=None):
    expected = {'tcp.payload_received'} if key in ('mail', 'console') else {'service.action'}
    if key == 'ssh':
        expected = {'service.session_io'}
    if key == 'redis':
        expected.add('service.auth_attempt')
    def find():
        recent = [row for row in rows(client, trap_id) if before is None or row['event_id'] not in before]
        sessions = {}
        for row in recent:
            if row['event_type'] in expected:
                session = row.get('session_id', row.get('source', {}).get('port'))
                sessions.setdefault(session, {})[row['event_type']] = row
        complete = [list(group.values()) for group in sessions.values() if expected <= group.keys()]
        direct = [group for group in complete if all(e.get('source', {}).get('port') == source_port for e in group)]
        if len(direct) == 1:
            return direct[0]
        if before is not None:
            require(len(complete) <= 1, 'Несколько новых сессий: нельзя однозначно определить события запроса')
            if complete:
                print('  NAT изменил исходный порт; события найдены по новой сессии.')
                return complete[0]
        return None
    return wait_for(find, f'События {key} от подключения :{source_port}', 60)


def custom_event(client, trap_id, marker, name):
    cache = {}
    def find():
        for row in rows(client, trap_id, 'http.custom'):
            eid = row['event_id']
            if eid not in cache:
                cache[eid] = client.request('GET', '/api/events/' + eid)
            event = cache[eid]
            data = event.get('data', {})
            payload = data.get('payload', {})
            if data.get('name') == name and isinstance(payload, dict) and payload.get('run') == marker:
                return event
        return None
    return wait_for(find, 'Событие ' + name, 90)
