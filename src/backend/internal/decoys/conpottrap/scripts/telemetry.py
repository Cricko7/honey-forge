def observation_text(value):
    if value is None:
        return ""
    if isinstance(value, bytes):
        # Conpot's TCP engines usually supply hex-encoded ASCII bytes.
        return value[:2048].decode("ascii", errors="backslashreplace")
    if isinstance(value, dict):
        return json.dumps(value, ensure_ascii=True, default=str)[:2048]
    return str(value)[:2048]


class Session:
    def __init__(self, registry, key):
        self.registry, self.key = registry, key
        self.id = str(uuid.uuid4())
        self.started = self.last = time.monotonic()
        self.received = 0
        self.closed = False

    def log_event(self, event_type=None, request=None, response=None, error=None, **extra):
        with self.registry.lock:
            if self.closed or event_type == "NEW_CONNECTION":
                return
            if event_type == "CONNECTION_CLOSED" and self.registry.transport == "tcp":
                # ENIP labels each response as closed; the socket owns lifetime.
                return
            if event_type in {"CONNECTION_LOST", "CONNECTION_CLOSED", "CONNECTION_QUIT", "CONNECTION_TERMINATED", "CONNECTION_FAILED"}:
                reason = "network_error" if event_type in {"CONNECTION_FAILED", "CONNECTION_TERMINATED"} else "peer_closed"
                self.close(reason)
                return
            self.last = time.monotonic()
            text = " ".join(filter(None, [str(event_type or "request")[:64],
                "request=" + observation_text(request) if request is not None else "",
                "response=" + observation_text(response) if response is not None else "",
                "protocol_error" if error is not None else ""]))
            encoded = text.encode("utf-8", errors="replace")
            received = 0
            if isinstance(request, (str, bytes)):
                try:
                    received = len(bytes.fromhex(request.decode("ascii") if isinstance(request, bytes) else request))
                except (ValueError, UnicodeError):
                    received = len(request)
            # JSON observations are not packet captures; only known request
            # bytes are counted. Unknown UDP wire lengths remain zero.
            received = min(received, 8192)
            if self.received + received > 1 << 20:
                raise RuntimeError("session observation capacity exceeded")
            self.registry.channel.emit({"kind": "action", "session_id": self.id,
                "input": encoded[:1024].decode("utf-8", errors="ignore"),
                "truncated": len(encoded) > 1024 or len(observation_text(request)) >= 2048,
                "received_bytes": received})
            self.received += received

    def close(self, reason):
        with self.registry.lock:
            if self.closed:
                return
            self.registry.channel.emit({"kind": "closed", "session_id": self.id,
                "bytes_received": self.received, "duration_ms": max(0, int((time.monotonic() - self.started) * 1000)), "reason": reason})
            self.closed = True
            self.registry.sessions.pop(self.key, None)


class Registry:
    def __init__(self, channel, capacity, max_seconds, transport):
        self.channel, self.capacity = channel, capacity
        self.max_seconds, self.transport = max_seconds, transport
        self.lock = threading.RLock()
        self.sessions = {}

    def get(self, protocol, source_ip, source_port, *destination):
        # Upstream groups some sessions by IP alone. Include source port so
        # concurrent clients behind the same NAT cannot mix their observations.
        key = (protocol, str(source_ip), int(source_port))
        with self.lock:
            if key not in self.sessions:
                if len(self.sessions) >= self.capacity:
                    raise RuntimeError("session capacity exceeded")
                session = Session(self, key)
                self.channel.emit({"kind": "opened", "session_id": session.id,
                    "source_ip": key[1], "source_port": key[2]})
                self.sessions[key] = session
            return self.sessions[key]

    def expire(self, now):
        with self.lock:
            for session in list(self.sessions.values()):
                if self.transport == "udp" and (now - session.started >= self.max_seconds or now - session.last >= 5):
                    session.close("idle_timeout")

    def close_all(self):
        with self.lock:
            for session in list(self.sessions.values()):
                session.close("service_stopped")
