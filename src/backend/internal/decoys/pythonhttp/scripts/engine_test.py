import asyncio
import json
import unittest
from engine import Engine, FlowError


def block(key, kind, config=None, **outputs):
    return {'id': key, 'kind': kind, 'config': config or {}, 'outputs': outputs}


class EngineTests(unittest.IsolatedAsyncioTestCase):
    async def test_send_passes_typed_json_and_continuation_to_agent(self):
        sent=[]
        async def forward(node_id, config, payload, data):
            sent.append((node_id,config['target_id'],payload,data['_pipeline_token']))
        async def emit(*args): pass
        engine=Engine([
            block('send','send',{'target_id':'target','service':'web','method':'POST','path':'/pipeline','payload':'{"count":"${json.count}"}'},out='reply'),
            block('reply','response',{'format':'json','body':'{}','status':202,'headers':'{}'}),
        ], forward=forward)
        result=await engine.execute('send',{'json':{'count':3},'_pipeline_token':'continuation'},emit)
        self.assertEqual(result['status'],202)
        self.assertEqual(sent,[('send','target',{'count':3},'continuation')])

    async def test_send_rejection_follows_error_branch(self):
        async def forward(*args):raise FlowError('target offline')
        async def emit(*args):pass
        engine=Engine([
            block('send','send',{'target_id':'target','payload':'{}'},out='reply',error='bad'),
            block('reply','response',{'format':'json','body':'{}','status':202,'headers':'{}'}),
            block('bad','response',{'format':'json','body':'{"error":"offline"}','status':503,'headers':'{}'}),
        ],forward=forward)
        response=await engine.execute('send',{},emit)
        self.assertEqual(response['status'],503)
        self.assertEqual(json.loads(response['body']),{'error':'offline'})

    async def test_event_only_on_selected_condition_branch(self):
        events = []
        async def emit(kind, data):
            events.append((kind, data))
        engine = Engine([
            block('check', 'condition', {'source': 'json.action', 'operator': 'equals', 'value': 'login'}, yes='notify', no='reply'),
            block('notify', 'event', {'name': 'login.processed', 'severity': 'high', 'payload': '{"count":"${json.count}","user":"${json.user}"}'}, out='reply'),
            block('reply', 'response', {'format': 'json', 'body': '{}', 'status': 200, 'headers': '{}'}),
        ])
        await engine.execute('check', {'json': {'action': 'other'}}, emit)
        self.assertFalse(any(kind == 'http.custom' for kind, _ in events))
        await engine.execute('check', {'json': {'action': 'login', 'count': 3, 'user': 'alice'}}, emit)
        custom = [data for kind, data in events if kind == 'http.custom']
        self.assertEqual(custom, [{'node_id': 'notify', 'name': 'login.processed', 'severity': 'high', 'payload': {'count': 3, 'user': 'alice'}}])

    async def test_event_rejects_oversized_payload_before_emitting(self):
        events = []
        async def emit(kind, data):
            events.append((kind, data))
        engine = Engine([
            block('notify', 'event', {'name': 'stage', 'severity': 'medium', 'payload': '"${body}"'}, out='reply', error='bad'),
            block('reply', 'response', {'format': 'json', 'body': '{}', 'status': 200, 'headers': '{}'}),
            block('bad', 'response', {'format': 'json', 'body': '{}', 'status': 422, 'headers': '{}'}),
        ])
        result = await engine.execute('notify', {'body': '\u0410' * 4096}, emit)
        self.assertEqual(result['status'], 422)
        self.assertFalse(any(kind == 'http.custom' for kind, _ in events))

    async def test_nested_call_and_typed_json(self):
        events = []
        async def emit(kind, data):
            events.append((kind, data))
        engine = Engine([
            block('a', 'input', out='parse'), block('parse', 'parse', {'format': 'json'}, out='call', error='bad'),
            block('call', 'call', {'target': 'b', 'payload': '"${json}"', 'key': 'result'}, out='reply'),
            block('b', 'input', out='upper'), block('upper', 'transform', {'key': 'user', 'operation': 'upper', 'value': '"${json.user}"'}, out='inner'),
            block('inner', 'response', {'format': 'json', 'body': '{"user":"${vars.user}","number":"${json.number}"}', 'status': 201, 'headers': '{}'}),
            block('reply', 'response', {'format': 'json', 'body': '"${vars.result}"', 'status': 200, 'headers': '{}'}),
            block('bad', 'response', {'format': 'json', 'body': '{"error":"bad JSON"}', 'status': 422, 'headers': '{}'}),
        ])
        response = await engine.execute('a', {'body': '{"user":"alice","number":3}', 'method': 'POST'}, emit)
        self.assertEqual(json.loads(response['body']), {'user': 'ALICE', 'number': 3})
        self.assertEqual(response['status'], 200)
        self.assertTrue(any(d.get('node_id') == 'b' for _, d in events))
        invalid = await engine.execute('a', {'body': '{'}, emit)
        self.assertEqual(invalid['status'], 422)

    async def test_fork_join_and_request_isolation(self):
        engine = Engine([
            block('a', 'input', out='fork'), block('fork', 'fork', branch1='one', branch2='two', out='join'),
            block('one', 'response', {'format': 'json', 'body': '1', 'status': 200, 'headers': '{}'}),
            block('two', 'response', {'format': 'json', 'body': '2', 'status': 200, 'headers': '{}'}),
            block('join', 'join', {'key': 'results'}, out='reply'),
            block('reply', 'response', {'format': 'json', 'body': '"${vars.results}"', 'status': 200, 'headers': '{}'}),
        ])
        async def emit(*args):
            pass
        replies = await asyncio.gather(*[engine.execute('a', {}, emit) for _ in range(10)])
        for reply in replies:
            self.assertEqual(json.loads(reply['body'])['branch2']['body'], 2)

    async def test_cycle_and_header_injection(self):
        async def emit(*args):
            pass
        engine = Engine([block('a', 'input', out='a')], max_steps=4)
        with self.assertRaises(FlowError):
            await engine.execute('a', {}, emit)
        engine = Engine([block('r', 'response', {'format': 'json', 'body': '{}', 'status': 200, 'headers': '{"X-Test":"${body}"}'})])
        with self.assertRaises(FlowError):
            await engine.execute('r', {'body': 'bad\r\nInjected: yes'}, emit)

    async def test_state_sessions_are_isolated(self):
        async def emit(*args):
            pass
        engine = Engine([
            block('set', 'state', {'operation': 'set', 'key': 'value', 'value': '"${body}"'}, out='reply'),
            block('get', 'state', {'operation': 'get', 'key': 'value', 'value': 'null'}, out='reply'),
            block('reply', 'response', {'format': 'json', 'body': '"${vars.value}"', 'status': 200, 'headers': '{}'}),
        ])
        await engine.execute('set', {'body': 'one', 'session': 'first'}, emit)
        await engine.execute('set', {'body': 'two', 'session': 'second'}, emit)
        self.assertEqual(json.loads((await engine.execute('get', {'session': 'first'}, emit))['body']), 'one')
        self.assertIsNone(json.loads((await engine.execute('get', {'session': 'unknown'}, emit))['body']))

    async def test_call_serializes_string_payload_for_receiver_parser(self):
        async def emit(*args):
            pass
        engine = Engine([
            block('a', 'call', {'target': 'b', 'payload': '"hello"', 'key': 'result'}, out='final'),
            block('b', 'input', out='parse'), block('parse', 'parse', {'format': 'json'}, out='reply'),
            block('reply', 'response', {'format': 'json', 'body': '"${json}"', 'status': 200, 'headers': '{}'}),
            block('final', 'response', {'format': 'json', 'body': '"${vars.result}"', 'status': 200, 'headers': '{}'}),
        ])
        response = await engine.execute('a', {}, emit)
        self.assertEqual(json.loads(response['body']), 'hello')


if __name__ == '__main__':
    unittest.main()
