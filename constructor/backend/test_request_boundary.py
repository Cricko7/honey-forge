import unittest
from unittest.mock import patch

from .test_integration import IntegrationTests


class RequestBoundaryTests(IntegrationTests):
    def test_invalid_json_values_do_not_change_board(self):
        chain = self.create()
        path = '/api/chains/' + chain
        before = self.client.get(path, headers=self.headers).json()
        for body, status in [
            ('{"viewport":{"x":1e309}}', 422),
            ('{"viewport":{"x":NaN}}', 400),
            ('{"nodes":[{"type":"custom/listener","config":{"note":"\\ud800"}}]}', 422),
            ('{"nodes":[{"type":"custom/listener","config":{"value":Infinity}}]}', 400),
            ('{"nodes":[{"type":"trap/tcp","config":{"_real":"oops"}}]}', 422),
            ('{"name":"one","name":"two"}', 400),
            ('{"nodes":[{"type":"x","config":{"a":1,"a":2}}]}', 400),
        ]:
            with self.subTest(body=body):
                response = self.client.patch(path, content=body, headers={**self.headers, 'Content-Type': 'application/json'})
                self.assertEqual(response.status_code, status, response.text)
                self.assertEqual(self.client.get(path, headers=self.headers).json(), before)
        self.assertEqual(self.client.post('/api/chains', json={'name': 'Still healthy'}, headers=self.headers).status_code, 201)

    def test_storage_failure_does_not_publish_create_or_update(self):
        chain = self.create()
        path = '/api/chains/' + chain
        before = self.client.get(path, headers=self.headers).json()
        count = len(self.client.get('/api/chains', headers=self.headers).json())
        with self.assertLogs('constructor.backend.app', level='ERROR'), patch('pathlib.Path.replace', side_effect=OSError('disk unavailable')):
            created = self.client.post('/api/chains', json={'name': 'Failed create'}, headers=self.headers)
            changed = self.client.patch(path, json={'name': 'Failed update'}, headers=self.headers)
        self.assertEqual(created.status_code, 503, created.text)
        self.assertEqual(changed.status_code, 503, changed.text)
        self.assertEqual(self.client.get(path, headers=self.headers).json(), before)
        self.assertEqual(len(self.client.get('/api/chains', headers=self.headers).json()), count)

    def test_legacy_malformed_binding_returns_client_error(self):
        from .models import Chain, Node
        from .runtime import trap_id
        from fastapi import HTTPException
        with self.assertRaises(HTTPException) as error:
            trap_id(Chain(), Node.model_construct(type='trap/tcp', id='bad', config={'_real': 'oops'}))
        self.assertEqual(error.exception.status_code, 422)
