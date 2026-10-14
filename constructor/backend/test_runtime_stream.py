import json
import unittest
from types import SimpleNamespace
from unittest.mock import AsyncMock, patch

from .models import Chain, Node
from .runtime import RuntimeManager


class RuntimeStreamTests(unittest.IsolatedAsyncioTestCase):
    async def test_reopening_skips_history_and_emits_only_new_events(self):
        chain = Chain(nodes=[Node(id='web', type='http/input', config={'port': 32181})],
                      bindings={'__http__': {'trap_id': '11111111-1111-4111-8111-111111111111'}})
        old = {'event_id': 'old', 'event_type': 'http.block', 'occurred_at': '2026-10-14T10:00:00Z',
               'destination': {'port': 32181}, 'data': {'node_id': 'parse'}}
        new = {**old, 'event_id': 'new', 'occurred_at': '2026-10-14T10:01:00Z'}
        pages = iter([{'items': [old]}, {'items': [new, old]}])

        def request(method, path):
            if path.startswith('/api/events?'):
                return next(pages)
            if path == '/api/events/new':
                return new
            if path == '/api/session':
                return {}
            raise AssertionError('Historical event details must not be replayed: ' + path)

        manager = RuntimeManager()
        with patch('constructor.backend.runtime.store', SimpleNamespace(get=lambda _: chain)), \
             patch.object(manager, 'status', return_value={}), \
             patch('constructor.backend.runtime.real_api._request', side_effect=request), \
             patch('constructor.backend.runtime.asyncio.sleep', new_callable=AsyncMock):
            stream = manager.stream(chain.id)
            try:
                first = json.loads((await anext(stream))[6:])
                second = json.loads((await anext(stream))[6:])
                third = json.loads((await anext(stream))[6:])
                self.assertEqual(first['kind'], 'hello')
                self.assertEqual(second['kind'], 'hello')
                self.assertEqual(third['event_id'], 'new')
                self.assertEqual(third['node_id'], 'parse')
            finally:
                await stream.aclose()
