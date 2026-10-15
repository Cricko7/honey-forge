"""Exercise the adapter without requiring native C bindings on the developer OS."""
import importlib.util
import pathlib
import sys
import types
import unittest

HERE = pathlib.Path(__file__).parent
core = types.ModuleType("dionaea.core")
core.ihandler = type("ihandler", (), {"__init__": lambda self, pattern: None})
package = types.ModuleType("dionaea")
package.__path__ = []
sys.modules.update({"dionaea": package, "dionaea.core": core})


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


channel = load("dionaea.local_channel", HERE.parents[1] / "internal/enginepipe/channel.py")
sessions = load("dionaea.local_sessions", HERE / "connections.py")
observer = load("dionaea.local_observer", HERE / "observer.py")


class FakeChannel:
    def __init__(self):
        import threading
        self.items = []
        self.stopping = threading.Event()

    def emit(self, item):
        self.items.append(item)


class NativeConnection:
    def __init__(self):
        self.remote = types.SimpleNamespace(host="192.0.2.1", port=41234)
        self.timeouts = types.SimpleNamespace(idle=0, sustain=0)
        self._in = types.SimpleNamespace(accounting=types.SimpleNamespace(limit=0))
        self._out = types.SimpleNamespace(accounting=types.SimpleNamespace(limit=0))
        self.disconnected = False
        self.inputs = []

    def handle_established(self):
        self.timeouts.idle = 9999

    def handle_io_in(self, data):
        self.inputs.append(data)
        return len(data) if data.endswith(b"\n") else 0

    def handle_disconnect(self):
        self.disconnected = True

    def close(self):
        self.handle_disconnect()


class AdapterTests(unittest.TestCase):
    def setUp(self):
        self.channel = FakeChannel()
        self.registry = sessions.Registry(self.channel, 1, 10)
        self.engine = sessions.observed_class(NativeConnection, self.registry)

    def test_fragments_are_counted_once_and_final_events_are_ordered(self):
        conn = self.engine()
        conn.handle_established()
        conn.handle_io_in(b"abc")
        conn.handle_io_in(b"abcdef\n")
        conn.handle_disconnect()
        self.assertEqual([m["kind"] for m in self.channel.items], ["opened", "action", "action", "closed"])
        self.assertEqual([m["received_bytes"] for m in self.channel.items[1:3]], [3, 4])
        self.assertEqual(self.channel.items[-1]["bytes_received"], 7)
        self.assertEqual(conn.timeouts.sustain, 10)
        self.assertEqual(conn._in.accounting.limit, 1 << 20)
        self.assertFalse(self.registry.sessions)

    def test_capacity_and_oversized_input_fail_closed(self):
        conn, extra = self.engine(), self.engine()
        conn.handle_established()
        extra.handle_established()
        self.assertTrue(extra.disconnected)
        conn.handle_io_in(b"x" * 8193)
        self.assertTrue(conn.disconnected)
        self.assertFalse(conn.inputs)
        self.assertEqual(self.channel.items[-1]["reason"], "network_error")

    def test_authentication_is_pinned_to_an_accepted_connection(self):
        conn = self.engine()
        conn.handle_established()
        handler = observer.AuthObserver(self.registry)
        handler.handle_incident(types.SimpleNamespace(origin="dionaea.modules.python.mysql.login", con=conn, username=b"guest", password=""))
        self.assertEqual(self.channel.items[-1]["kind"], "auth")
        self.assertEqual(self.channel.items[-1]["username"], "guest")
        self.assertIsNone(self.channel.items[-1]["password"])
        count = len(self.channel.items)
        handler.handle_incident(types.SimpleNamespace(origin="dionaea.modules.python.mssql.login", con=object(), username="secret", password="secret"))
        self.assertEqual(len(self.channel.items), count)

    def test_stop_closes_every_session_and_observations_are_bounded(self):
        conn = self.engine()
        conn.handle_established()
        conn.handle_io_in(b"x" * 4096)
        self.assertLessEqual(len(self.channel.items[-1]["input"].encode()), 1024)
        self.assertTrue(self.channel.items[-1]["truncated"])
        self.registry.close_all()
        self.assertEqual(self.channel.items[-1]["reason"], "service_stopped")
        conn.handle_disconnect()
        self.assertEqual(len([m for m in self.channel.items if m["kind"] == "closed"]), 1)

    def test_engine_choices_have_no_external_handlers(self):
        self.assertEqual(set(observer.ENGINES), {"smb", "mssql", "mysql", "mqtt", "http", "memcache"})
        self.assertEqual(observer.engine_config("mysql")["databases"]["information_schema"]["path"], ":memory:")
        self.assertFalse(observer.engine_config("http")["soap_enabled"])

    def test_cleanup_failure_still_closes_the_observed_session(self):
        class BrokenCleanup(NativeConnection):
            def handle_disconnect(self):
                raise RuntimeError("native cleanup failed")
        conn = sessions.observed_class(BrokenCleanup, self.registry)()
        conn.handle_established()
        self.assertFalse(conn.handle_disconnect())
        self.assertFalse(self.registry.sessions)
        self.assertEqual(self.channel.items[-1]["kind"], "closed")


if __name__ == "__main__":
    unittest.main()
