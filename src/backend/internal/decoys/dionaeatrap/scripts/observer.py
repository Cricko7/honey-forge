"""Only this module is imported by the native engine; no upstream exporters."""
import importlib
import json
import logging
import os
import sys
import threading
import time

from dionaea.core import ihandler
from dionaea.local_channel import Channel
from dionaea.local_sessions import Registry, observed_class

ENGINES = {
    "smb": ("dionaea.smb.smb", "smbd"),
    "mssql": ("dionaea.mssql.mssql", "mssqld"),
    "mysql": ("dionaea.mysql.mysql", "mysqld"),
    "mqtt": ("dionaea.mqtt.mqtt", "mqttd"),
    "http": ("dionaea.http", "httpd"),
    "memcache": ("dionaea.memcache", "Memcache"),
}
channel = registry = listener = auth = bootstrap = None


def engine_config(protocol):
    if protocol == "mysql":
        return {"databases": {"information_schema": {"path": ":memory:"}}}
    if protocol == "http":
        return {"root": "/opt/dionaea/share/http-root", "max_request_size": 8,
                "soap_enabled": False, "template": {"enabled": False}}
    return {}


def clipped(value):
    if value is None:
        return None, False
    if isinstance(value, bytes):
        value = value[:4096].decode("utf-8", errors="replace")
    encoded = str(value).encode("utf-8", errors="replace")
    return encoded[:1024].decode("utf-8", errors="ignore"), len(encoded) > 1024


class AuthObserver(ihandler):
    def __init__(self, sessions):
        ihandler.__init__(self, "dionaea.modules.python.*")
        self.registry = sessions

    def handle_incident(self, incident):
        origin = incident.origin
        if origin not in {"dionaea.modules.python.mssql.login", "dionaea.modules.python.mysql.login",
                          "dionaea.modules.python.mqtt.connect"}:
            return
        with self.registry.lock:
            session = self.registry.sessions.get(incident.con)
            if session is None or self.registry.channel.stopping.is_set():
                return
            username, user_cut = clipped(getattr(incident, "username", None))
            # Upstream MySQL reports an empty password, not the authentication
            # response. Preserve its absence rather than inventing a credential.
            password, pass_cut = clipped(None if origin.endswith("mysql.login") else getattr(incident, "password", None))
            self.registry.channel.emit({"kind": "auth", "session_id": session["id"], "username": username or "",
                                        "password": password, "truncated": user_cut or pass_cut})


def supervise():
    try:
        while not channel.stopping.wait(0.05):
            if channel.failed.is_set():
                os._exit(1)
        registry.close_all()
        deadline = time.monotonic() + 5
        while channel.pending.unfinished_tasks and time.monotonic() < deadline:
            if channel.failed.wait(0.01):
                os._exit(1)
        # C sockets and the helper process close with the process. Calling native
        # close from this thread would race the libev loop; never do that here.
        os._exit(1 if channel.pending.unfinished_tasks or channel.failed.is_set() else 0)
    except BaseException:
        os._exit(1)


def new():
    global bootstrap, channel, registry
    logging.disable(logging.CRITICAL)
    try:
        bootstrap = json.loads(sys.stdin.readline(16385))
        config = bootstrap["config"]
        channel = Channel(sys.stdin, os.fdopen(3, "w", buffering=1, encoding="utf-8"))
        registry = Registry(channel, config["session"]["max_connections"], config["session"]["max_seconds"])
    except BaseException:
        os._exit(1)


def start():
    global listener, auth
    try:
        service = bootstrap["config"]["services"][0]
        module, name = ENGINES[service["protocol"]]
        engine = getattr(importlib.import_module(module), name)
        listener = observed_class(engine, registry)()
        listener.apply_config(engine_config(service["protocol"]))
        auth = AuthObserver(registry)
        if not listener.bind(bootstrap["host"], service["port"]) or not listener.listen():
            raise RuntimeError("listener unavailable")
        channel.output.write('{"kind":"ready"}\n')
        channel.output.flush()
        for target in (channel.read_control, channel.write_events, supervise):
            threading.Thread(target=target, daemon=True).start()
    except BaseException:
        os._exit(1)
