"""Conpot TCP accept adapter with bounded clients and deterministic shutdown."""
import asyncio
import socket
from types import SimpleNamespace


class GuardedSocket:
    def __init__(self, sock, received=None):
        self.sock, self.received = sock, 0
        self.on_receive = received

    def __getattr__(self, name):
        return getattr(self.sock, name)

    def recv(self, count, *flags):
        if count > 8192:
            raise OSError("request exceeds capacity")
        flag = flags[0] if flags else 0
        waitall = bool(flag & socket.MSG_WAITALL)
        flag &= ~socket.MSG_WAITALL
        value = self.sock.recv(count, flag)
        while waitall and value and len(value) < count:
            part = self.sock.recv(count - len(value), flag)
            if not part:
                break
            value += part
        self.received += len(value)
        if self.received > 1 << 20:
            raise OSError("session exceeds capacity")
        if value and self.on_receive is not None:
            self.on_receive(value)
        return value


def bounded_tcp(registry, max_connections, max_seconds, protocol):
    async def serve(host, port, handler_obj, *, stop_event, ready_event=None, **unused):
        loop = asyncio.get_running_loop()
        listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        clients, tasks = set(), set()
        listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        try:
            listener.bind((host, port))
            listener.listen(max_connections)
            listener.setblocking(False)
            bound = listener.getsockname()
            handler_obj.server = SimpleNamespace(server_host=bound[0], server_port=bound[1])
            handler_obj.server_host, handler_obj.server_port = bound
            if ready_event is not None:
                ready_event.set()

            async def handle(conn, peer):
                conn.setblocking(True)
                conn.settimeout(5)
                def disconnect():
                    try:
                        conn.shutdown(socket.SHUT_RDWR)
                    except OSError:
                        pass
                deadline = loop.call_later(max_seconds, disconnect)
                try:
                    observer = None
                    if protocol == "enip":
                        session = registry.get(protocol, peer[0], peer[1])
                        observer = lambda data: session.log_event(event_type="ENIP", request=data.hex())
                    await asyncio.to_thread(handler_obj.handle, GuardedSocket(conn, observer), peer)
                finally:
                    deadline.cancel()
                    conn.close()
                    clients.discard(conn)
                    with registry.lock:
                        for session in list(registry.sessions.values()):
                            if session.key[1:] == (str(peer[0]), int(peer[1])):
                                session.close("service_stopped" if stop_event.is_set() else "peer_closed")

            while not stop_event.is_set():
                try:
                    conn, peer = await asyncio.wait_for(loop.sock_accept(listener), timeout=0.1)
                except asyncio.TimeoutError:
                    continue
                if len(clients) >= max_connections:
                    conn.close()
                    continue
                clients.add(conn)
                task = asyncio.create_task(handle(conn, peer))
                tasks.add(task)
                def completed(task):
                    tasks.discard(task)
                    if not task.cancelled() and task.exception() is not None:
                        registry.channel.failed.set()
                task.add_done_callback(completed)
        finally:
            listener.close()
            for conn in list(clients):
                try:
                    conn.shutdown(socket.SHUT_RDWR)
                except OSError:
                    pass
            if tasks:
                await asyncio.gather(*tasks, return_exceptions=True)
    return serve


def bounded_udp(registry):
    async def serve(host, port, handler_obj, *, stop_event, ready_event=None, **unused):
        loop = asyncio.get_running_loop()
        peer = None

        class Datagram(asyncio.DatagramProtocol):
            def datagram_received(self, data, address):
                nonlocal peer
                if len(data) > 8192 or registry.channel.failed.is_set():
                    return
                peer = address
                try:
                    handler_obj.handle(data, address)
                except RuntimeError:
                    # Session capacity is bounded; reject the new request.
                    pass
                finally:
                    peer = None

        transport, _ = await loop.create_datagram_endpoint(Datagram, local_addr=(host, port))
        def reply(data, address):
            # Only unicast replies to the datagram currently being handled.
            if address == peer and len(data) <= 8192:
                transport.sendto(data, address)
        bound = transport.get_extra_info("sockname")
        handler_obj.server = SimpleNamespace(server_host=bound[0], server_port=bound[1], sendto=reply)
        handler_obj.server_host, handler_obj.server_port = bound
        if ready_event is not None:
            ready_event.set()
        try:
            await stop_event.wait()
        finally:
            transport.close()
    return serve
