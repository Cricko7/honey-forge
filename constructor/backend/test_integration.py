import os
import tempfile
import unittest
from unittest.mock import patch

os.environ['CONSTRUCTOR_DATA_DIR'] = tempfile.mkdtemp()

from fastapi.testclient import TestClient
from . import real_api
from .app import app


class IntegrationTests(unittest.TestCase):
    def setUp(self):
        self.org = '00000000-0000-0000-0000-000000000001'
        self.role = 'admin'
        self.calls = []
        self.apply_pending = False
        self.mock = patch.object(real_api, '_request', side_effect=self.request)
        self.mock.start()
        self.addCleanup(self.mock.stop)
        self.client = TestClient(app)
        self.headers = {'Cookie': 'session=test', 'Origin': 'https://localhost:8443', 'X-CSRF-Token': 'csrf'}

    def request(self, method, path, body=None, headers=None):
        self.calls.append((method, path, body))
        if path == '/api/session':
            if not real_api.request_headers.get().get('Cookie'):
                raise real_api.RealApiError(401, 'unauthenticated', 'Sign in')
            return {'organization': {'id': self.org}, 'user': {'role': self.role}, 'csrf_token': 'csrf'}
        if path.startswith('/api/trap-types'):
            return {'items': [{'type_id': tid, 'type_version': 1, 'available_for_new_profiles': True} for tid in ('tcp-banner', 'custom-tcp', 'custom-http')]}
        if path == '/api/profiles' and method == 'POST':
            return {'id': 'profile-1', 'revision': 1}
        if path == '/api/traps' and method == 'POST':
            return {'id': '11111111-1111-4111-8111-111111111111'}
        if path == '/api/traps/11111111-1111-4111-8111-111111111111':
            return {'id': '11111111-1111-4111-8111-111111111111', 'runtime_state': 'stopped', 'connectivity': 'offline'}
        if path == '/api/profiles/profile-1':
            return {'id': 'profile-1', 'revision': 1 if method == 'GET' else 2}
        if path.endswith('/commands') and method == 'POST':
            if body['action'] == 'apply_config':
                self.apply_pending = True
            elif body['action'] == 'start' and self.apply_pending:
                raise real_api.RealApiError(409, 'command_in_progress', 'Apply config is still running')
            return {'id': 'command-1'}
        if path.endswith('/commands/command-1'):
            self.apply_pending = False
            return {'status': 'succeeded'}
        if path.startswith('/api/events'):
            return {'items': []}
        return {'items': []}

    def create(self):
        response = self.client.post('/api/chains', json={'name': 'Test'}, headers=self.headers)
        self.assertEqual(response.status_code, 201, response.text)
        return response.json()['id']

    def test_auth_and_csrf(self):
        self.assertEqual(self.client.get('/api/chains').status_code, 401)
        self.assertEqual(self.client.post('/api/chains', json={}, headers={'Cookie': 'session=test'}).status_code, 403)
        self.role = 'viewer'
        self.assertEqual(self.client.post('/api/chains', json={}, headers=self.headers).status_code, 403)

    def test_organization_isolation(self):
        chain = self.create()
        self.org = '00000000-0000-0000-0000-000000000002'
        self.assertEqual(self.client.get('/api/chains/' + chain, headers=self.headers).status_code, 404)

    def test_build_deploy_retry_and_actual_state(self):
        chain = self.create()
        nodes = [{'id': 'tcp', 'type': 'custom/listener', 'config': {'port': 32222, 'name': 'mail'}},
                 {'id': 'banner', 'type': 'custom/banner', 'config': {'text': '220 Mail\r\n'}},
                 {'id': 'rule', 'type': 'custom/rule', 'config': {'match': 'prefix', 'input': 'HELO', 'response': '250 Hello\r\n'}}]
        edges = [{'source': 'tcp', 'source_port': 'out', 'target': 'banner', 'target_port': 'in'},
                 {'source': 'banner', 'source_port': 'out', 'target': 'rule', 'target_port': 'in'}]
        response = self.client.patch('/api/chains/' + chain, json={'nodes': nodes, 'edges': edges}, headers=self.headers)
        self.assertEqual(response.status_code, 200, response.text)
        for _ in range(2):
            response = self.client.post('/api/chains/' + chain + '/deploy', json={'action': 'deploy'}, headers=self.headers)
            self.assertEqual(response.status_code, 200, response.text)
            self.assertEqual(response.json()['node_status']['tcp'], 'stopped')
        self.assertEqual(sum(path == '/api/traps' and method == 'POST' for method, path, _ in self.calls), 1)
        profile = next(body for method, path, body in self.calls if path == '/api/profiles' and method == 'POST')
        self.assertEqual(profile['config']['listeners'][0]['port'], 32222)
        self.assertEqual(profile['type_id'], 'custom-tcp')
        self.assertEqual(profile['config']['listeners'][0]['rules'][0]['response'], '250 Hello\r\n')

    def test_invalid_port_has_no_side_effects(self):
        chain = self.create()
        self.client.patch('/api/chains/' + chain, json={'nodes': [{'type': 'custom/listener', 'config': {'port': 70000}}]}, headers=self.headers)
        response = self.client.post('/api/chains/' + chain + '/deploy', json={'action': 'deploy'}, headers=self.headers)
        self.assertEqual(response.status_code, 422, response.text)
        self.assertFalse(any(path == '/api/profiles' for _, path, _ in self.calls))

    def test_unconnected_rule_rejected(self):
        chain = self.create()
        self.client.patch('/api/chains/' + chain, json={'nodes': [{'id': 'listener', 'type': 'custom/listener', 'config': {'name': 'mail', 'port': 32222}}, {'type': 'custom/rule'}]}, headers=self.headers)
        response = self.client.post('/api/chains/' + chain + '/deploy', json={}, headers=self.headers)
        self.assertEqual(response.status_code, 422, response.text)

    def test_auth_under_proxy_prefix(self):
        client = TestClient(app, root_path='/constructor')
        self.assertEqual(client.get('/constructor/api/chains').status_code, 401)

    def test_upstream_profile_creation_uses_current_session(self):
        response = self.client.post('/api/real/traps', json={'request_id': '22222222-2222-4222-8222-222222222222',
            'name': 'Profile trap', 'profile_id': '33333333-3333-4333-8333-333333333333'}, headers=self.headers)
        self.assertEqual(response.status_code, 201, response.text)
        self.assertTrue(response.json()['ok'])
        self.role = 'viewer'
        self.assertEqual(self.client.post('/api/real/traps', json={}, headers=self.headers).status_code, 403)

    def test_changed_rules_apply_new_profile_revision(self):
        chain = self.create()
        nodes = [{'id': 'tcp', 'type': 'custom/listener', 'config': {'name': 'custom', 'port': 32222}}]
        self.client.patch('/api/chains/' + chain, json={'nodes': nodes}, headers=self.headers)
        self.assertEqual(self.client.post('/api/chains/' + chain + '/deploy', json={}, headers=self.headers).status_code, 200)
        nodes[0]['config']['port'] = 32223
        self.client.patch('/api/chains/' + chain, json={'nodes': nodes}, headers=self.headers)
        response = self.client.post('/api/chains/' + chain + '/deploy', json={}, headers=self.headers)
        self.assertEqual(response.status_code, 200, response.text)
        self.assertTrue(any(method == 'PATCH' and path == '/api/profiles/profile-1' for method, path, _ in self.calls))
        self.assertTrue(any(body and body.get('action') == 'apply_config' and body['params']['profile_revision'] == 2 for _, _, body in self.calls))

    def test_http_deploy_update_stop_and_authorization(self):
        from .test_http_builder import HTTPBuilderTests
        chain = self.create()
        board = HTTPBuilderTests().board()
        data = {'nodes': [n.model_dump() for n in board.nodes], 'edges': [e.model_dump() for e in board.edges]}
        self.assertEqual(self.client.patch('/api/chains/' + chain, json=data, headers=self.headers).status_code, 200)
        result = self.client.post('/api/chains/' + chain + '/deploy', json={}, headers=self.headers)
        self.assertEqual(result.status_code, 200, result.text)
        self.assertIn('web', result.json()['node_status'])
        profile = next(body for method, path, body in self.calls if method == 'POST' and path == '/api/profiles')
        self.assertEqual(profile['type_id'], 'custom-http')
        self.assertEqual(profile['config']['blocks'][3]['config']['target'], 'other')
        data['nodes'][6]['config']['status'] = 201
        self.client.patch('/api/chains/' + chain, json=data, headers=self.headers)
        result = self.client.post('/api/chains/' + chain + '/deploy', json={}, headers=self.headers)
        self.assertEqual(result.status_code, 200, result.text)
        self.assertTrue(any(body and body.get('action') == 'apply_config' for _, _, body in self.calls))
        self.assertEqual(self.client.post('/api/chains/' + chain + '/deploy', json={'action': 'stop'}, headers=self.headers).status_code, 200)
        self.assertTrue(any(body and body.get('action') == 'stop' for _, _, body in self.calls))
        self.role = 'viewer'
        self.assertEqual(self.client.post('/api/chains/' + chain + '/deploy', json={}, headers=self.headers).status_code, 403)


if __name__ == '__main__':
    unittest.main()
