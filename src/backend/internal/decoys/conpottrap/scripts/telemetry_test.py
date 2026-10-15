import io
import pathlib
import socket
import unittest


scope = {"__name__": "adapter_test"}
exec((pathlib.Path(__file__).parents[2] / "internal/enginepipe/channel.py").read_text(), scope)
exec(pathlib.Path(__file__).with_name("telemetry.py").read_text(), scope)
exec(pathlib.Path(__file__).with_name("network.py").read_text(), scope)
Registry = scope["Registry"]
Channel = scope["Channel"]


class Collector:
    def __init__(self):
        self.items = []

    def emit(self, item):
        self.items.append(item)


class TelemetryTests(unittest.TestCase):
    def test_stop_request_keeps_ack_channel_open_for_final_events(self):
        channel = Channel(io.StringIO("stop\nok\n"), io.StringIO())
        channel.read_control()
        self.assertTrue(channel.stopping.is_set())
        self.assertFalse(channel.failed.is_set())
        self.assertTrue(channel.acks.get_nowait())

    def test_fragmented_s7_header_is_read_exactly_without_platform_waitall(self):
        class Fragmented:
            def __init__(self):
                self.parts = [b"\x03", b"\x00", b"\x00\x16"]
            def recv(self, count, flags=0):
                self_flags.append(flags)
                return self.parts.pop(0)
        self_flags = []
        wrapped = scope["GuardedSocket"](Fragmented())
        self.assertEqual(wrapped.recv(4, socket.MSG_WAITALL), b"\x03\x00\x00\x16")
        self.assertTrue(all(flag & socket.MSG_WAITALL == 0 for flag in self_flags))

    def test_concurrent_clients_from_same_ip_are_separate_sessions(self):
        channel = Collector()
        registry = Registry(channel, 2, 300, "tcp")
        a = registry.get("modbus", "192.0.2.1", 40001)
        b = registry.get("modbus", "192.0.2.1", 40002)
        self.assertNotEqual(a.id, b.id)
        a.log_event(request=b"000100000006010100010001")
        a.close("peer_closed")
        b.close("peer_closed")
        self.assertEqual([e["kind"] for e in channel.items], ["opened", "opened", "action", "closed", "closed"])
        self.assertEqual(channel.items[-2]["bytes_received"], 12)
        self.assertEqual(len(registry.sessions), 0)

    def test_session_cap_and_duplicate_close(self):
        channel = Collector()
        registry = Registry(channel, 1, 300, "udp")
        a = registry.get("snmp", "192.0.2.1", 40001)
        with self.assertRaises(RuntimeError):
            registry.get("snmp", "192.0.2.2", 40002)
        a.close("peer_closed")
        a.close("peer_closed")
        self.assertEqual(len(channel.items), 2)

    def test_hostile_input_is_bounded_in_utf8_bytes(self):
        channel = Collector()
        registry = Registry(channel, 1, 300, "tcp")
        a = registry.get("s7comm", "192.0.2.1", 40001)
        a.log_event(event_type="S7", request="\u0000😀" * 10000)
        data = channel.items[-1]
        self.assertLessEqual(len(data["input"].encode()), 1024)
        self.assertTrue(data["truncated"])
        self.assertLessEqual(data["received_bytes"], 8192)

    def test_overflow_fails_closed_instead_of_silently_dropping(self):
        channel = Channel(io.StringIO(), io.StringIO())
        for _ in range(64):
            channel.emit({"kind": "action"})
        with self.assertRaises(RuntimeError):
            channel.emit({"kind": "action"})
        self.assertTrue(channel.failed.is_set())

    def test_expired_datagram_sessions_close_and_release_capacity(self):
        channel = Collector()
        registry = Registry(channel, 1, 5, "udp")
        a = registry.get("snmp", "192.0.2.1", 40001)
        registry.expire(a.started + 6)
        self.assertEqual(channel.items[-1]["reason"], "idle_timeout")
        self.assertEqual(len(registry.sessions), 0)


if __name__ == "__main__":
    unittest.main()
