import http.client
import base64
import zlib
import json
from pathlib import Path
import queue
import socket
import subprocess
import sys
import threading
import unittest
import uuid
import datetime


class ServerTests(unittest.TestCase):
    def setUp(self):
        probe = socket.socket()
        probe.bind(('127.0.0.1', 0))
        self.port = probe.getsockname()[1]
        probe.close()
        self.events = []
        self.frames = []
        self.ready = queue.Queue()
        self.completed = queue.Queue()
        root = Path(__file__).parent
        code = '\n'.join((root / name).read_text(encoding='utf-8') for name in ('engine.py','pipeline_server.py','server.py'))
        code = "import base64,zlib;exec(zlib.decompress(base64.b64decode(%r)))" % base64.b64encode(zlib.compress(code.encode())).decode()
        self.process = subprocess.Popen([sys.executable, '-I', '-u', '-c', code], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        self.addCleanup(self.cleanup)
        self.thread = threading.Thread(target=self.control, daemon=True)
        self.thread.start()
        config = {'services': [{'name': 'web', 'port': self.port, 'entry': 'web'}], 'blocks': [
            {'id': 'web', 'kind': 'input', 'config': {'method': 'POST', 'path': '/api'}, 'outputs': {'out': 'parse'}},
            {'id': 'parse', 'kind': 'parse', 'config': {'format': 'json'}, 'outputs': {'out': 'reply', 'error': 'error'}},
            {'id': 'reply', 'kind': 'response', 'config': {'status': 201, 'format': 'json', 'body': '"${json}"', 'headers': '{"X-Trap":"custom"}'}, 'outputs': {}},
            {'id': 'error', 'kind': 'response', 'config': {'status': 422, 'format': 'json', 'body': '{"error":"invalid JSON"}', 'headers': '{}'}, 'outputs': {}},
        ], 'limits': {'max_steps': 512, 'timeout_ms': 10000, 'max_body_bytes': 128}}
        self.process.stdin.write(json.dumps({'snapshot': {'config': config, 'type_version': 2, 'profile_revision': 1}, 'host': '127.0.0.1'}) + '\n')
        self.process.stdin.flush()
        self.ready.get(timeout=5)

    def control(self):
        for line in self.process.stdout:
            message = json.loads(line)
            if message['type'] == 'ready':
                self.ready.put(True)
            elif message['type'] == 'traffic':
                self.frames.append(message['frame'])
            else:
                event = message['event']
                self.events.append(event)
                if event['event_type'] == 'http.response' and 'pipeline_id' in event['data']:
                    self.completed.put(event)
                try:
                    self.process.stdin.write(json.dumps({'event_id': event['event_id'], 'ok': True}) + '\n')
                    self.process.stdin.flush()
                except (ValueError, BrokenPipeError):
                    return

    def cleanup(self):
        self.process.stdin.close()
        try:
            self.process.wait(timeout=8)
        except subprocess.TimeoutExpired:
            self.process.kill()
            self.process.wait(timeout=3)
        self.thread.join(timeout=1)
        self.process.stdout.close()
        self.process.stderr.close()

    def test_requests_errors_and_telemetry(self):
        for name, method, path, body, status in [
            ('JSON', 'POST', '/api', '{"count":3}', 201),
            ('malformed JSON', 'POST', '/api', '{', 422),
            ('method', 'GET', '/api', '', 405),
            ('path', 'POST', '/missing', '{}', 404),
            ('body size', 'POST', '/api', 'x' * 129, 413),
        ]:
            with self.subTest(name=name):
                connection = http.client.HTTPConnection('127.0.0.1', self.port, timeout=5)
                try:
                    connection.request(method, path, body=body, headers={'Content-Type': 'application/json'})
                    response = connection.getresponse()
                    payload = response.read()
                    self.assertEqual(response.status, status)
                    self.assertEqual(response.getheader('Content-Type'), 'application/json')
                    if status == 201:
                        self.assertEqual(json.loads(payload), {'count': 3})
                        self.assertEqual(response.getheader('X-Trap'), 'custom')
                finally:
                    connection.close()
        self.assertTrue(any(e['event_type'] == 'http.block' and e['data']['phase'] == 'failed' for e in self.events))
        self.assertTrue(any(e['event_type'] == 'http.failure' for e in self.events))
        self.assertTrue(any(f['kind'] == 'data' for f in self.frames))
        for session in {e['session_id'] for e in self.events}:
            events = [e for e in self.events if e['session_id'] == session]
            self.assertEqual([e['session_sequence'] for e in events], list(range(1, len(events) + 1)))

    def test_chunked_request_rejected(self):
        connection = http.client.HTTPConnection('127.0.0.1', self.port, timeout=5)
        try:
            connection.request('POST', '/api', body=iter([b'{}']), encode_chunked=True)
            response = connection.getresponse()
            self.assertEqual(response.status, 400)
            response.read()
        finally:
            connection.close()

    def test_pipeline_completion_and_duplicate_delivery(self):
        delivery = {'request_id': str(uuid.uuid4()), 'token': str(uuid.uuid4()),
                    'source_id': str(uuid.uuid4()), 'trace_id': str(uuid.uuid4()), 'hop': 1,
                    'source_ip': '127.0.0.1', 'source_port': 1234, 'service': 'web',
                    'method': 'POST', 'path': '/api', 'payload': {'count': 3},
                    'expires_at': (datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(minutes=1)).isoformat()}
        message = json.dumps({'type': 'pipeline_receive', 'delivery': delivery}) + '\n'
        self.process.stdin.write(message + message)
        self.process.stdin.flush()
        event = self.completed.get(timeout=5)
        self.assertEqual(event['data']['pipeline_id'], delivery['trace_id'])
        self.assertEqual(event['data']['status'], 201)
        # A normal HTTP round trip is a barrier on the same event loop.
        connection = http.client.HTTPConnection('127.0.0.1', self.port, timeout=5)
        try:
            connection.request('POST', '/api', body='{}')
            connection.getresponse().read()
        finally:
            connection.close()
        self.assertEqual(sum(e['event_type'] == 'http.pipeline_received' for e in self.events), 1)


if __name__ == '__main__':
    unittest.main()
