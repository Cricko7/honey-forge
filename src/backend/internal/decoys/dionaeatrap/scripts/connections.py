"""Bound native callbacks without calling the C event loop from worker threads."""
import threading
import time
import uuid


class Registry:
    def __init__(self, channel, capacity, max_seconds):
        self.channel, self.capacity, self.max_seconds = channel, capacity, max_seconds
        self.sessions = {}
        self.lock = threading.RLock()

    def open(self, conn):
        with self.lock:
            if self.channel.stopping.is_set() or len(self.sessions) >= self.capacity:
                return False
            session = {"id": str(uuid.uuid4()), "started": time.monotonic(), "received": 0, "pending": 0}
            self.channel.emit({"kind": "opened", "session_id": session["id"],
                               "source_ip": conn.remote.host, "source_port": conn.remote.port})
            self.sessions[conn] = session
            return True

    def input(self, conn, data):
        with self.lock:
            session = self.sessions.get(conn)
            if session is None or self.channel.stopping.is_set():
                return False
            # Native input includes unconsumed bytes from the previous callback.
            delta = data[session["pending"]:]
            if len(data) > 8192 or session["received"] + len(delta) > 1 << 20:
                return False
            if delta:
                text = delta[:512].hex()
                self.channel.emit({"kind": "action", "session_id": session["id"], "input": text,
                                   "truncated": len(delta) > 512, "received_bytes": len(delta)})
                session["received"] += len(delta)
            return True

    def consumed(self, conn, length, consumed):
        with self.lock:
            session = self.sessions.get(conn)
            if session is not None:
                session["pending"] = max(0, length - consumed)

    def close(self, conn, reason):
        with self.lock:
            session = self.sessions.get(conn)
            if session is None:
                return
            self.channel.emit({"kind": "closed", "session_id": session["id"], "reason": reason,
                               "duration_ms": max(0, int((time.monotonic() - session["started"]) * 1000)),
                               "bytes_received": session["received"]})
            del self.sessions[conn]

    def close_all(self):
        with self.lock:
            for conn in list(self.sessions):
                self.close(conn, "service_stopped")


def observed_class(engine, registry):
    class Observed(engine):
        def handle_established(self):
            if not registry.open(self):
                self.close()
                return
            try:
                super().handle_established()
                self.timeouts.idle = min(30, registry.max_seconds)
                self.timeouts.sustain = registry.max_seconds
                self._in.accounting.limit = self._out.accounting.limit = 1 << 20
                # httpd does not inherit this setting from its listener.
                self.max_request_size = 8192
            except Exception:
                registry.close(self, "network_error")
                self.close()

        def handle_io_in(self, data):
            if not registry.input(self, data):
                registry.close(self, "network_error")
                self.close()
                return len(data)
            try:
                consumed = super().handle_io_in(data)
                if not isinstance(consumed, int) or not 0 <= consumed <= len(data):
                    raise ValueError("invalid native consumption")
                registry.consumed(self, len(data), consumed)
                return consumed
            except Exception:
                registry.close(self, "network_error")
                self.close()
                return len(data)

        def handle_disconnect(self):
            registry.close(self, "peer_closed")
            # Native cleanup releases temporary upload files and SQLite state.
            # A rejected connection may not have completed native setup.
            try:
                super().handle_disconnect()
            except Exception:
                pass
            return False

        def handle_timeout_idle(self):
            registry.close(self, "idle_timeout")
            return False

        def handle_timeout_sustain(self):
            registry.close(self, "idle_timeout")
            return False

    return Observed
