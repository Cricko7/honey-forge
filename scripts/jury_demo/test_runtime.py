"""Repository-only contract checks against the real compiler and interpreter."""
import asyncio
import importlib.util
import io
import json
from pathlib import Path
import sys
import unittest
from unittest.mock import Mock, patch

from api import Client
from events import protocol_events, Dashboard
from probes import resp
from scenarios import boards, config, plan

REPO = Path(__file__).resolve().parents[2]


class BoundaryTests(unittest.TestCase):
    def test_dashboard_uses_dns_fallback_and_verified_tls(self):
        client = Client('https://localhost:8443')
        raw, tls, ws = Mock(), Mock(), Mock()
        client.context = Mock()
        client.context.wrap_socket.return_value = tls
        ws.recv.return_value = '{"type":"stream.ready"}'
        with patch('events.socket.create_connection', return_value=raw) as connect, patch('websocket.create_connection', return_value=ws) as create:
            with Dashboard(client):
                pass
        connect.assert_called_once_with(('localhost', 8443), timeout=20)
        client.context.wrap_socket.assert_called_once_with(raw, server_hostname='localhost')
        self.assertIs(create.call_args.kwargs['socket'], tls)
        ws.close.assert_called_once()

    def test_resp_success_nil_and_truncation(self):
        for raw, value in [(b'+OK\r\n', b'+OK'), (b'$3\r\nhey\r\n', b'hey'), (b'$-1\r\n', None)]:
            self.assertEqual(resp(io.BytesIO(raw)), value)
        for raw in (b'$90000\r\n', b'$3\r\na', b'OK', b'$-2\r\n'):
            with self.subTest(raw=raw), self.assertRaises((ValueError, RuntimeError)):
                resp(io.BytesIO(raw))

    def test_events_correlate_to_exact_connection(self):
        rows = [{'event_id': 'old', 'source': {'port': 1}, 'event_type': 'service.action'},
                {'event_id': 'new', 'source': {'port': 2}, 'event_type': 'service.action'}]
        with patch('events.rows', return_value=rows):
            self.assertEqual(protocol_events(Mock(), 'trap', 2, 'modbus')[0]['event_id'], 'new')

    def test_nat_uses_one_new_session_and_ignores_history(self):
        rows = [{'event_id': 'old', 'session_id': 'before', 'source': {'port': 99}, 'event_type': 'service.action'},
                {'event_id': 'opened', 'session_id': 'after', 'source': {'port': 88}, 'event_type': 'service.connection_opened'},
                {'event_id': 'action', 'session_id': 'after', 'source': {'port': 88}, 'event_type': 'service.action'}]
        with patch('events.rows', return_value=rows):
            events = protocol_events(Mock(), 'trap', 2, 'http', before={'old'})
        self.assertEqual([e['event_id'] for e in events], ['action'])

    def test_nat_rejects_ambiguous_sessions(self):
        rows = [{'event_id': str(i), 'session_id': str(i), 'source': {'port': 88}, 'event_type': 'service.action'} for i in (1, 2)]
        with patch('events.rows', return_value=rows), patch('events.wait_for', side_effect=lambda find, *_: find()):
            with self.assertRaises(RuntimeError):
                protocol_events(Mock(), 'trap', 2, 'http', before=set())

    def test_rest_event_alone_does_not_pass_dashboard_check(self):
        dashboard = Dashboard(Mock())
        dashboard.read = Mock(side_effect=[{'type': 'event.created', 'payload': {'data': {'event': {'event_id': 'other'}}}},
                                          RuntimeError('closed')])
        with self.assertRaises(RuntimeError):
            dashboard.delivered(['expected'])

    def test_dashboard_matches_persisted_event_ids(self):
        dashboard = Dashboard(Mock())
        dashboard.read = Mock(return_value={'type': 'event.created', 'payload': {'data': {'event': {'event_id': 'expected'}}}})
        dashboard.delivered(['expected'])

    def test_login_is_prompted_and_authorization_failure_prevents_writes(self):
        client = Client('https://example.org')
        client.request = Mock(side_effect=[{'csrf_token': 'csrf'}, {'user': {'role': 'viewer'}}])
        with patch('builtins.input', return_value='operator@example.org'), patch('api.getpass.getpass', return_value='runtime-only'):
            with self.assertRaises(RuntimeError):
                client.login()
        self.assertEqual([call.args[:2] for call in client.request.call_args_list],
                         [('POST', '/api/sessions'), ('GET', '/api/session')])


@unittest.skipUnless((REPO / 'constructor/backend/models.py').exists(), 'Only in source repository')
class RealRuntimeTests(unittest.IsolatedAsyncioTestCase):
    @classmethod
    def setUpClass(cls):
        sys.path.insert(0, str(REPO))
        from constructor.backend.models import Chain
        from constructor.backend.http_builder import compile_http
        cls.compiled = {r: compile_http(Chain(**b)).config['compiled'] for r, b in boards(34000).items()}
        path = REPO / 'src/backend/internal/decoys/pythonhttp/scripts/engine.py'
        spec = importlib.util.spec_from_file_location('jury_engine', path)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        cls.engine_class = module.Engine

    async def test_both_risk_branches_preserve_typed_payload_and_join(self):
        for score, name, severity in [(25, 'jury.gateway.backup', 'high'), (90, 'jury.gateway.high_risk', 'critical')]:
            with self.subTest(score=score):
                events, transferred = [], []
                async def emit(kind, data):
                    if kind == 'http.custom':
                        events.append(data)
                gateway = self.engine_class(self.compiled['gateway']['blocks'])
                async def forward(node_id, cfg, payload, data):
                    transferred.append(payload)
                    await gateway.execute('gateway', {'body': json.dumps(payload), 'method': 'POST', 'session': 'target'}, emit)
                portal = self.engine_class(self.compiled['portal']['blocks'], forward=forward)
                result = await portal.execute('portal', {'body': json.dumps({'action': 'login', 'user': 'Alice', 'score': score, 'run': 'test'}),
                                                          'method': 'POST', 'session': 'source'}, emit)
                self.assertEqual(result['status'], 202)
                reply = json.loads(result['body'])
                self.assertTrue(reply['queued'])
                self.assertEqual(set(reply['results']), {'branch1', 'branch2', 'branch3'})
                self.assertEqual(transferred, [{'action': 'backup', 'user': 'alice', 'score': score, 'run': 'test'}])
                event = next(e for e in events if e['name'] == name)
                self.assertEqual(event['payload']['score'], score)
                self.assertEqual(event['severity'], severity)

    async def test_invalid_payload_never_reaches_receiver(self):
        forward = Mock()
        async def emit(*_):
            pass
        portal = self.engine_class(self.compiled['portal']['blocks'], forward=forward)
        for body in ('{', '{"action":"unknown","user":"Jury"}', '{"action":"login"}'):
            result = await portal.execute('portal', {'body': body, 'method': 'POST'}, emit)
            self.assertEqual(result['status'], 422)
        forward.assert_not_called()

    async def test_direct_gateway_health_keeps_correlation_marker(self):
        events = []
        async def emit(kind, data):
            if kind == 'http.custom':
                events.append(data)
        gateway = self.engine_class(self.compiled['gateway']['blocks'])
        result = await gateway.execute('gateway', {'body': json.dumps({'action': 'status', 'user': 'Jury', 'score': 10, 'run': 'direct'}),
                                                   'method': 'POST', 'session': 'health'}, emit)
        self.assertEqual(result['status'], 200)
        self.assertEqual(json.loads(result['body'])['health']['status'], 'ok')
        event = next(e for e in events if e['name'] == 'jury.gateway.health')
        self.assertEqual(event['payload']['run'], 'direct')

    def test_profiles_match_published_schemas(self):
        import jsonschema
        for item in plan(34000):
            path = REPO / 'src/backend/modules/catalog/schemas' / (item['type_id'] + '-1.json')
            schema = json.loads(path.read_text())['config_schema']
            jsonschema.validate(config(item, 'generated-only-for-test'), schema)


if __name__ == '__main__':
    unittest.main()
