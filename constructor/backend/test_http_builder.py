import unittest
from unittest.mock import AsyncMock, patch
from types import SimpleNamespace
from fastapi import HTTPException
from .models import Chain, Node, Edge
from .custom import compile_chain, registry_types


class HTTPBuilderTests(unittest.TestCase):
    def board(self):
        return Chain(nodes=[
            Node(id='web', type='http/input', config={'name': 'web', 'port': 32001, 'method': 'POST', 'path': '/login'}),
            Node(id='parse', type='http/parse', config={'format': 'json'}),
            Node(id='check', type='http/condition', config={'source': 'json.action', 'operator': 'equals', 'value': 'login'}),
            Node(id='call', type='http/call', config={'target': 'other', 'payload': '{"user":"${json.user}"}', 'key': 'result'}),
            Node(id='other', type='http/input', config={'name': 'other', 'port': 32002, 'method': 'ANY', 'path': '/'}),
            Node(id='upper', type='http/transform', config={'key': 'user', 'value': '"${json.user}"', 'operation': 'upper'}),
            Node(id='ok', type='http/response', config={'format': 'json', 'body': '{"user":"${vars.user}"}', 'status': 200, 'headers': '{}'}),
            Node(id='final', type='http/response', config={'format': 'json', 'body': '"${vars.result}"', 'status': 200, 'headers': '{}'}),
            Node(id='no', type='http/response', config={'format': 'json', 'body': '{"error":"unknown action"}', 'status': 422, 'headers': '{}'}),
        ], edges=[Edge(source=a, source_port=p, target=b, target_port='in') for a, p, b in [
            ('web', 'out', 'parse'), ('parse', 'out', 'check'), ('check', 'yes', 'call'),
            ('check', 'no', 'no'), ('call', 'out', 'final'), ('other', 'out', 'upper'), ('upper', 'out', 'ok')]])

    def test_compile_branch_and_call(self):
        built = compile_chain(self.board())
        self.assertEqual(built.type, 'build/custom-http')
        config = built.config['compiled']
        self.assertEqual(len(config['services']), 2)
        self.assertEqual(config['blocks'][2]['outputs']['yes'], 'call')
        self.assertEqual(config['services'][0]['entry'], 'web')

    def test_invalid_graphs(self):
        for name, change in [
            ('missing branch', lambda c: c.edges.pop(3)),
            ('terminal output', lambda c: c.edges.append(Edge(source='ok', source_port='out', target='web', target_port='in'))),
            ('invalid json', lambda c: c.nodes[6].config.update(body='{')),
            ('duplicate ports', lambda c: c.nodes[4].config.update(port=32001)),
            ('missing target', lambda c: c.nodes[3].config.update(target='missing')),
        ]:
            with self.subTest(name=name):
                board = self.board()
                change(board)
                with self.assertRaises(HTTPException):
                    compile_chain(board)

    def test_palette(self):
        types = {s['type']: s for s in registry_types()}
        self.assertEqual([p['id'] for p in types['http/condition']['ports'] if p['kind'] == 'out'], ['yes', 'no', 'error'])
        self.assertIn('http/input', types)
        self.assertIn('http/transform', types)
        self.assertIn('http/response', types)
        self.assertIn('http/event', types)

    def test_compile_event_and_reject_invalid_configuration(self):
        for name, config, valid in [
            ('valid', {'name': 'login.processed', 'severity': 'high', 'payload': '{"user":"${json.user}"}'}, True),
            ('invalid name', {'name': '${json.name}'}, False),
            ('invalid severity', {'severity': 'urgent'}, False),
            ('invalid payload', {'payload': '{'}, False),
        ]:
            with self.subTest(name=name):
                board = self.board()
                board.nodes.append(Node(id='notify', type='http/event', config=config))
                board.edges[0].target = 'notify'
                board.edges.append(Edge(source='notify', source_port='out', target='parse', target_port='in'))
                if valid:
                    compiled = compile_chain(board).config['compiled']
                    self.assertEqual(compiled['blocks'][-1]['kind'], 'event')
                else:
                    with self.assertRaises(HTTPException):
                        compile_chain(board)

    def test_compile_separate_trap_send_and_validate_destination_shape(self):
        for name,config,valid in [
            ('valid',{'target_id':'11111111-1111-4111-8111-111111111111','service':'web','method':'POST','path':'/pipeline','payload':'{"count":"${json.count}"}'},True),
            ('invalid trap',{'target_id':'http://localhost/'},False),
            ('invalid endpoint',{'target_id':'11111111-1111-4111-8111-111111111111','path':'//localhost/'},False),
            ('query',{'target_id':'11111111-1111-4111-8111-111111111111','path':'/ingest?token=x'},False),
            ('bad json',{'target_id':'11111111-1111-4111-8111-111111111111','payload':'{'},False),
        ]:
            with self.subTest(name=name):
                board=self.board()
                board.nodes.append(Node(id='send',type='http/send',config=config))
                board.edges[0].target='send'
                board.edges.append(Edge(source='send',source_port='out',target='parse',target_port='in'))
                if valid:
                    compiled=compile_chain(board).config['compiled']
                    self.assertEqual(compiled['blocks'][-1]['kind'],'send')
                else:
                    with self.assertRaises(HTTPException):compile_chain(board)


class HTTPStreamTests(unittest.IsolatedAsyncioTestCase):
    async def test_summary_loads_block_detail_for_board_highlight(self):
        from .runtime import RuntimeManager
        from . import real_api
        chain = Chain(nodes=[Node(id='web', type='http/input', config={'port': 32001}), Node(id='upper', type='http/transform')],
                      bindings={'__http__': {'trap_id': '11111111-1111-4111-8111-111111111111'}})
        summary = {'event_id': 'event-1', 'event_type': 'http.block', 'destination': {'port': 32001}, 'occurred_at': '2026-10-14T12:00:00Z'}
        pages = iter([{'items': []}, {'items': [summary]}])
        def request(method, path):
            if path == '/api/session':
                return {}
            if path.startswith('/api/traps/'):
                return {'id': chain.bindings['__http__']['trap_id'], 'runtime_state': 'running'}
            if path == '/api/events/event-1':
                return {**summary, 'data': {'node_id': 'upper'}}
            return next(pages)
        with patch('constructor.backend.runtime.store', SimpleNamespace(get=lambda _: chain)), patch.object(real_api, '_request', side_effect=request), patch('constructor.backend.runtime.asyncio.sleep', new_callable=AsyncMock):
            stream = RuntimeManager().stream(chain.id)
            try:
                await anext(stream)
                await anext(stream)
                hop = await anext(stream)
                self.assertIn('"node_id": "upper"', hop)
            finally:
                await stream.aclose()
