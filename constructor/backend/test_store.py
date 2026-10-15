import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from .models import ChainCreate, ChainUpdate
from .store import ChainStore


class StoreTests(unittest.TestCase):
    def test_failed_writes_preserve_memory_and_disk(self):
        with tempfile.TemporaryDirectory() as directory, patch('constructor.backend.store.DATA_DIR', Path(directory)):
            store = ChainStore('test')
            original = store.create(ChainCreate(name='Original'))
            for action in [lambda: store.create(ChainCreate(name='New')),
                           lambda: store.update(original.id, ChainUpdate(name='Changed')),
                           lambda: store.delete(original.id),
                           lambda: store.duplicate(original.id)]:
                with self.subTest(action=action), patch.object(Path, 'replace', side_effect=OSError('disk unavailable')):
                    with self.assertRaises(OSError):
                        action()
                self.assertEqual([c.name for c in store.list()], ['Original'])
                self.assertEqual([c.name for c in ChainStore('test').list()], ['Original'])

    def test_get_does_not_expose_mutable_storage(self):
        with tempfile.TemporaryDirectory() as directory, patch('constructor.backend.store.DATA_DIR', Path(directory)):
            store = ChainStore('test')
            original = store.create(ChainCreate(name='Original'))
            read = store.get(original.id)
            read.name = 'Changed without save'
            self.assertEqual(store.get(original.id).name, 'Original')
