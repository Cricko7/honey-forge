import json
import unittest
from engine import Engine, FlowError


class EmbeddedTests(unittest.IsolatedAsyncioTestCase):
    async def test_redis_result_can_be_returned_over_http(self):
        async def embedded(node, payload):
            self.assertEqual(node, 'redis')
            self.assertEqual(payload['commands'], [['SET', 'name', 'alice'], ['GET', 'name']])
            return {'body': ['OK', 'alice'], 'status': 200, 'headers': {}, 'events': []}
        emitted=[]
        async def emit(kind, data): emitted.append((kind, data))
        engine=Engine([
            {'id':'redis','kind':'redis','config':{'commands':'[["SET","name","${json.name}"],["GET","name"]]','key':'result'},'outputs':{'out':'reply'}},
            {'id':'reply','kind':'response','config':{'body':'"${vars.result}"','format':'json','status':200,'headers':'{}'},'outputs':{}},
        ], embedded=embedded)
        response=await engine.execute('redis', {'json':{'name':'alice'}}, emit)
        self.assertEqual(json.loads(response['body']), ['OK','alice'])
        self.assertTrue(any(d.get('name') == 'redis.executed' for _,d in emitted))

    async def test_unavailable_embedded_trap_uses_error_branch(self):
        async def embedded(*args): raise FlowError('unavailable')
        async def emit(*args): pass
        engine=Engine([
            {'id':'redis','kind':'redis','config':{'commands':'[["PING"]]','key':'result'},'outputs':{'out':'reply','error':'reply'}},
            {'id':'reply','kind':'response','config':{'body':'{}','format':'json','status':503,'headers':'{}'},'outputs':{}},
        ], embedded=embedded)
        self.assertEqual((await engine.execute('redis',{},emit))['status'],503)
