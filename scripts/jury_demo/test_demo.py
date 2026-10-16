import json
import ssl
from pathlib import Path
import unittest
from unittest.mock import Mock, patch

from scenarios import boards, plan, choose_ports
from api import origin, Client, ApiError


class DemoTests(unittest.TestCase):
    def test_ten_traps_and_disjoint_ports(self):
        items = plan(34000)
        self.assertEqual(len(items), 8)
        graphs = boards(34000)
        self.assertEqual(len(graphs), 2)
        ports = [x['port'] for x in items]
        for board in graphs.values():
            ports += [n['config']['port'] for n in board['nodes'] if n['type'] == 'http/input']
        self.assertEqual(len(ports), len(set(ports)))
        self.assertEqual(choose_ports({34000, 34025}, 34000), 34040)

    def test_graphs_have_no_dangling_edges_or_old_bindings(self):
        for name, board in boards(34000).items():
            with self.subTest(name=name):
                self.assertGreaterEqual(len(board['nodes']), 25)
                self.assertGreaterEqual(len(board['edges']), 35)
                ids = {n['id'] for n in board['nodes']}
                self.assertEqual(len(ids), len(board['nodes']))
                outputs = set()
                for edge in board['edges']:
                    self.assertIn(edge['source'], ids)
                    self.assertIn(edge['target'], ids)
                    key = (edge['source'], edge['source_port'])
                    self.assertNotIn(key, outputs)
                    outputs.add(key)
                self.assertNotIn('bindings', board)
                for node in board['nodes']:
                    self.assertNotIn('_real', node['config'])
                    self.assertNotIn('target_id', node['config'])
        sender = next(n for n in boards(34000)['portal']['nodes'] if n['type'] == 'http/send')
        self.assertEqual(sender['config']['path'], '/gateway')
        self.assertEqual(json.loads(sender['config']['payload'])['action'], 'backup')

    def test_credentials_url_rejected_and_site_path_normalized(self):
        self.assertEqual(origin('https://example.org/app/network'), 'https://example.org')
        for url in ['http://example.org', 'https://a:b@example.org', 'file:///a', 'https://example.org?a=b']:
            with self.subTest(url=url), self.assertRaises(ValueError):
                origin(url)

    def test_api_error_never_includes_response_secrets(self):
        client = Client('https://example.org')
        connection = Mock()
        response = connection.getresponse.return_value
        response.status = 401
        response.getheaders.return_value = []
        response.read.return_value = b'{"error":{"code":"unauthenticated","message":"secret"}}'
        with patch('api.http.client.HTTPSConnection', return_value=connection):
            with self.assertRaises(ApiError) as caught:
                client.request('GET', '/api/session')
        self.assertNotIn('secret', str(caught.exception))
        connection.close.assert_called_once()

    def test_tls_handshake_retry_sends_mutation_only_once(self):
        client = Client('https://example.org')
        first, second = Mock(), Mock()
        first.connect.side_effect = ssl.SSLEOFError('handshake closed')
        response = second.getresponse.return_value
        response.status, response.getheaders.return_value, response.read.return_value = 201, [], b'{}'
        with patch('api.http.client.HTTPSConnection', side_effect=[first, second]):
            client.request('POST', '/api/traps', {'request_id': 'same'})
        first.request.assert_not_called()
        first.close.assert_called_once()
        second.request.assert_called_once()

    def test_uncertain_mutation_is_never_retried(self):
        client = Client('https://example.org')
        connection = Mock()
        connection.getresponse.side_effect = ssl.SSLEOFError('response lost')
        with patch('api.http.client.HTTPSConnection', return_value=connection) as factory:
            with self.assertRaises(ssl.SSLEOFError):
                client.request('POST', '/api/traps', {'request_id': 'same'})
        factory.assert_called_once()
        connection.request.assert_called_once()

    def test_read_can_retry_a_lost_response(self):
        client = Client('https://example.org')
        with patch.object(client, '_request', side_effect=[ssl.SSLEOFError('response lost'), {'id': 'existing'}]) as request:
            self.assertEqual(client.request('GET', '/api/traps/existing'), {'id': 'existing'})
        self.assertEqual(request.call_count, 2)

    def test_no_mutation_on_read_only_preflight(self):
        from demo import preflight
        client = Mock()
        def respond(method, path, body=None):
            self.assertEqual(method, 'GET')
            if path.startswith('/api/trap-types'):
                return {'items': [{'type_id': t, 'available_for_new_profiles': True, 'type_version': 1}
                                  for t in ['conpot', 'dionaea', 'tcp-banner', 'redis-emulator', 'ssh-high-interaction', 'custom-http']]}
            if path.startswith('/constructor'):
                return []
            return {'items': [], 'next_cursor': None}
        client.request.side_effect = respond
        client.items.side_effect = lambda path: respond('GET', path)['items']
        catalog, port = preflight(client, 34000)
        self.assertIn('conpot', catalog)
        self.assertEqual(port, 34000)


if __name__ == '__main__':
    unittest.main()
