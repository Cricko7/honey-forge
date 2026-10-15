"""Managed Python HTTP server; configuration and acknowledged telemetry use pipes."""
import asyncio
import base64
import datetime
import http.cookies
import ipaddress
import json
import sys
import threading
import uuid
from urllib.parse import urlsplit, parse_qs


def send(message):
    sys.stdout.write(json.dumps(message, ensure_ascii=True, allow_nan=False, separators=(',', ':')) + '\n')
    sys.stdout.flush()


def captured_path(path):
    # RawMessage preserves JSON escapes on the way to the 16 KiB local frame.
    # Reserve room for the 4096-byte body capture, envelope and pipeline metadata.
    # Go also escapes HTML-sensitive characters when re-encoding the event.
    low, high = 0, min(len(path), 2048)
    while low < high:
        end = (low + high + 1) // 2
        encoded = json.dumps(path[:end], ensure_ascii=True)
        for char in '<>&':
            encoded = encoded.replace(char, '\\u%04x' % ord(char))
        if len(encoded) - 2 <= 4096:
            low = end
        else:
            high = end - 1
    return path[:low]


class Server(PipelineServerMixin):
    def __init__(self, bootstrap):
        self.snapshot = bootstrap['snapshot']
        self.config = self.snapshot['config']
        limits = self.config['limits']
        self.engine = Engine(self.config['blocks'], **limits, forward=self.forward_pipeline, embedded=self.call_embedded)
        self.blocks = self.engine.blocks
        self.host = bootstrap['host']
        self.pending = {}
        self.pipeline_pending = {}
        self.embedded_pending = {}
        self.pipeline_tasks = set()
        self.pipeline_seen = {}
        self.stop = asyncio.Event()
        self.ready = asyncio.Event()
        self.connections = set()
        self.max_body = limits['max_body_bytes']

    async def emit(self, event):
        key = event['event_id']
        future = asyncio.get_running_loop().create_future()
        self.pending[key] = future
        try:
            send({'type': 'event', 'event': event})
            async with asyncio.timeout(5):
                ok = await future
            if not ok:
                raise RuntimeError('telemetry rejected')
        except (TimeoutError, RuntimeError, BrokenPipeError):
            self.stop.set()
            raise
        finally:
            self.pending.pop(key, None)

    async def call_embedded(self, node_id, payload):
        key = str(uuid.uuid4())
        future = asyncio.get_running_loop().create_future()
        self.embedded_pending[key] = future
        try:
            send({'type': 'embedded_call', 'embedded': {'id': key, 'node_id': node_id, 'payload': payload}})
            async with asyncio.timeout(6):
                reply = await future
            if not reply.get('ok'):
                raise FlowError('embedded trap unavailable or request rejected')
            return reply['result']
        except TimeoutError as error:
            raise FlowError('embedded trap timeout') from error
        finally:
            self.embedded_pending.pop(key, None)

    def ack(self, message):
        if 'embedded_id' in message:
            future = self.embedded_pending.get(message['embedded_id'])
            if future and not future.done():
                future.set_result(message)
            return
        if message.get('type') == 'pipeline_receive':
            now = datetime.datetime.now(datetime.timezone.utc)
            self.pipeline_seen = {key: expiry for key, expiry in self.pipeline_seen.items() if expiry > now}
            delivery = message['delivery']
            key = delivery['token']
            expiry = datetime.datetime.fromisoformat(delivery['expires_at'].replace('Z', '+00:00'))

            if key in self.pipeline_seen or expiry <= now:
                return

            if len(self.pipeline_tasks) < 16:
                self.pipeline_seen[key] = expiry
                task=asyncio.create_task(self.receive_pipeline(delivery))
                self.pipeline_tasks.add(task)
                task.add_done_callback(self.pipeline_tasks.discard)
            return
        if 'pipeline_request_id' in message:
            future=self.pipeline_pending.get(message['pipeline_request_id'])
            if future and not future.done():
                future.set_result(message.get('accepted') is True)
            return
        future = self.pending.get(message.get('event_id'))
        if future and not future.done():
            future.set_result(message.get('ok') is True)

    def control(self, loop):
        try:
            for line in sys.stdin:
                if len(line) > 65536:
                    break
                message = json.loads(line)
                loop.call_soon_threadsafe(self.ack, message)
        except (ValueError, OSError):
            pass
        finally:
            loop.call_soon_threadsafe(self.stop.set)

    async def connection(self, service, reader, writer):
        await self.ready.wait()
        if len(self.connections) >= 64:
            writer.close()
            await writer.wait_closed()
            return
        task = asyncio.current_task()
        self.connections.add(task)
        session = str(uuid.uuid4())
        peer = writer.get_extra_info('peername')
        started = asyncio.get_running_loop().time()
        sequence, received = 0, 0
        wire_sequence = 0
        def mirror(kind, payload=b''):
            nonlocal wire_sequence
            local = writer.get_extra_info('sockname')
            address = lambda addr: f'[{addr[0]}]:{addr[1]}' if ':' in addr[0] else f'{addr[0]}:{addr[1]}'
            chunks = [payload[i:i+8192] for i in range(0, len(payload), 8192)] if kind == 'data' else [b'']
            for chunk in chunks:
                wire_sequence += 1
                frame = {'connection_id': session, 'sequence': wire_sequence, 'kind': kind,
                         'remote_address': address(peer), 'local_address': address(local)}
                if chunk:
                    frame['data'] = base64.b64encode(chunk).decode()
                send({'type': 'traffic', 'frame': frame})
        async def emit(kind, data):
            nonlocal sequence
            sequence += 1
            await self.emit({'event_id': str(uuid.uuid4()), 'event_type': kind,
                'type_id': 'custom-http', 'type_version': self.snapshot['type_version'], 'profile_revision': self.snapshot['profile_revision'],
                'occurred_at': datetime.datetime.now(datetime.timezone.utc).isoformat().replace('+00:00', 'Z'),
                'session_id': session, 'session_sequence': sequence,
                'source': {'ip': str(ipaddress.ip_address(peer[0])), 'port': peer[1]},
                'destination': {'protocol': 'tcp', 'port': service['port']}, 'data': {'service': service['name'], **data}})
        reason = 'peer_closed'
        request = {'method': 'GET'}
        try:
            mirror('open')
            await emit('service.connection_opened', {})
            response = {'status': 400, 'body': '{"error":"invalid request"}', 'format': 'json', 'headers': {}}
            try:
                async with asyncio.timeout(10):
                    raw = await reader.readuntil(b'\r\n\r\n')
                    mirror('data', raw)
                    received += len(raw)
                    if len(raw) > 16384:
                        raise FlowError('headers exceed limit')
                    lines = raw.decode('latin-1').split('\r\n')
                    method, target, version = lines[0].split(' ')
                    if version not in ('HTTP/1.0', 'HTTP/1.1') or method not in ('GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'):
                        raise FlowError('unsupported HTTP request')
                    headers = {}
                    for line in lines[1:-2]:
                        key, item = line.split(':', 1)
                        key = key.lower()
                        if not re.fullmatch(r'[!#$%&\x27*+.^_`|~0-9a-z-]+', key) or key in headers:
                            raise FlowError('invalid or duplicate header')
                        headers[key] = item.strip()
                    if 'transfer-encoding' in headers:
                        raise FlowError('use Content-Length for request bodies')
                    length = headers.get('content-length', '0')
                    if not length.isdecimal():
                        raise FlowError('invalid content length')
                    length = int(length)
                    if length > self.max_body:
                        response['status'] = 413
                        raise FlowError('body exceeds limit')
                    body = await reader.readexactly(length)
                    mirror('data', body)
                    received += len(body)
                    parsed = urlsplit(target)
                    if parsed.scheme or parsed.netloc or not parsed.path.startswith('/'):
                        raise FlowError('request target must be an absolute path')
                    cookies = http.cookies.SimpleCookie()
                    cookies.load(headers.get('cookie', ''))
                    cookie_values = {k: v.value for k, v in cookies.items()}
                    candidate = cookie_values.get('hf_session', '')
                    # Only server-issued sessions may resume state; unknown cookies get a new ID.
                    state_session = candidate if candidate in self.engine.sessions else str(uuid.uuid4())
                    request = {'method': method, 'path': parsed.path, 'body': body.decode('utf-8'), 'headers': headers,
                               'query': {k: v[-1] for k, v in parse_qs(parsed.query, max_num_fields=256).items()},
                               'cookies': cookie_values, 'session': state_session, 'request_id': session}
                    request['_source_ip'],request['_source_port']=peer[0],peer[1]
                    await emit('http.request', {'method': method, 'path': captured_path(parsed.path),
                        'payload_base64': base64.b64encode(body[:4096]).decode(), 'captured_bytes': min(len(body), 4096),
                        'original_bytes': len(body), 'truncated': len(body) > 4096})
                entry = self.blocks[service['entry']]['config']
                if entry['method'] != 'ANY' and entry['method'] != request['method']:
                    response = {'status': 405, 'body': '{"error":"method not allowed"}', 'format': 'json', 'headers': {'Allow': entry['method']}}
                elif entry['path'] != '*' and entry['path'] != request['path']:
                    response = {'status': 404, 'body': '{"error":"not found"}', 'format': 'json', 'headers': {}}
                else:
                    try:
                        response = await self.engine.execute(service['entry'], request, emit)
                    except (FlowError, TimeoutError) as error:
                        response = {'status': 504 if isinstance(error, TimeoutError) else 422, 'body': '{"error":"flow failed"}', 'format': 'json', 'headers': {}}
                        await emit('http.failure', {'message': 'timeout' if isinstance(error, TimeoutError) else str(error)[:256]})
                if state_session in self.engine.sessions:
                    response['session_cookie'] = 'hf_session=' + state_session + '; HttpOnly; SameSite=Lax; Path=/'
            except (ValueError, asyncio.IncompleteReadError, asyncio.LimitOverrunError, TimeoutError, http.cookies.CookieError) as error:
                await emit('http.failure', {'message': 'invalid request: ' + type(error).__name__})
            encoded = response['body'].encode('utf-8')
            # 204/304 and HEAD never put a body on the wire.
            if response['status'] in (204, 304):
                encoded = b''
            headers = {'Content-Type': {'json': 'application/json', 'html': 'text/html; charset=utf-8', 'text': 'text/plain; charset=utf-8'}[response['format']],
                       'Cache-Control': 'no-store', 'X-Content-Type-Options': 'nosniff', 'Server': 'nginx'}
            for key, value in response['headers'].items():
                for previous in [name for name in headers if name.lower() == key.lower()]:
                    del headers[previous]
                headers[key] = value
            safe_headers(headers)
            status = response['status']
            head = f'HTTP/1.1 {status} Response\r\n' + ''.join(f'{k}: {v}\r\n' for k, v in headers.items())
            if response.get('session_cookie'):
                head += 'Set-Cookie: ' + response['session_cookie'] + '\r\n'
            head += f'Content-Length: {len(encoded)}\r\nConnection: close\r\n\r\n'
            sent = b'' if request['method'] == 'HEAD' else encoded
            await emit('http.response', {'status': status, 'bytes_sent': len(sent),
                'payload_base64': base64.b64encode(sent[:4096]).decode(), 'truncated': len(sent) > 4096})
            writer.write(head.encode('latin-1') + (b'' if request['method'] == 'HEAD' else encoded))
            async with asyncio.timeout(5):
                await writer.drain()
        except asyncio.CancelledError:
            reason = 'service_stopped'
        except (OSError, RuntimeError, TimeoutError):
            reason = 'network_error'
        finally:
            try:
                await emit('service.connection_closed', {'duration_ms': max(0, int((asyncio.get_running_loop().time() - started) * 1000)),
                    'bytes_received': received, 'reason': reason})
            except (OSError, RuntimeError, TimeoutError):
                self.stop.set()
            writer.close()
            try:
                await writer.wait_closed()
            except OSError:
                pass
            self.connections.discard(task)
            mirror('close')

    async def run(self):
        loop = asyncio.get_running_loop()
        threading.Thread(target=self.control, args=(loop,), daemon=True).start()
        servers = []
        try:
            for service in self.config['services']:
                handler = lambda reader, writer, s=service: self.connection(s, reader, writer)
                servers.append(await asyncio.start_server(handler, self.host, service['port'], limit=16384))
            send({'type': 'ready'})
            self.ready.set()
            await self.stop.wait()
        finally:
            for server in servers:
                server.close()
            for server in servers:
                await server.wait_closed()
            tasks = list(self.connections | self.pipeline_tasks)
            for task in tasks:
                task.cancel()
            if tasks:
                await asyncio.gather(*tasks, return_exceptions=True)


if __name__ == '__main__':
    try:
        bootstrap = json.loads(sys.stdin.readline(1048577))
        asyncio.run(Server(bootstrap).run())
    except Exception as error:
        print('Python HTTP runtime stopped: ' + str(error), file=sys.stderr)
        sys.exit(1)
