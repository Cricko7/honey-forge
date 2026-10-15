import unittest
from fastapi import HTTPException
from .models import Chain, Node, Edge
from .custom import compile_chain


class EmbeddedTests(unittest.TestCase):
    def board(self, kind, config):
        return Chain(nodes=[Node(id='web', type='http/input'), Node(id='bait', type='http/'+kind, config=config), Node(id='reply', type='http/response')], edges=[Edge(source=a, source_port='out', target=b, target_port='in') for a,b in [('web','bait'),('bait','reply')]])

    def test_embedded_blocks_compile(self):
        for kind in ['redis', 'honeytoken']:
            with self.subTest(kind=kind):
                config=compile_chain(self.board(kind, {})).config['compiled']
                self.assertEqual(config['blocks'][1]['kind'], kind)

    def test_invalid_tokens_rejected(self):
        for tokens in ['[]', '{}', '[{"id":"x","kind":"url","path":"http://evil","value":"x"}]']:
            with self.subTest(tokens=tokens), self.assertRaises(HTTPException):
                compile_chain(self.board('honeytoken', {'tokens':tokens}))
