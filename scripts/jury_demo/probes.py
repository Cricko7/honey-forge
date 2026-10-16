"""Small bounded protocol requests, exclusively to the newly created listeners."""
import http.client
import json
import socket
import time


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def http_probe(host, port, path, body=None, raw=None, method='POST'):
    conn = http.client.HTTPConnection(host, port, timeout=15)
    try:
        conn.connect()
        source_port = conn.sock.getsockname()[1]
        payload = raw if raw is not None else json.dumps(body).encode() if body is not None else None
        conn.request(method, path, payload, {'Content-Type': 'application/json', 'Connection': 'close'})
        response = conn.getresponse()
        data = response.read(65537)
        require(len(data) <= 65536, 'Слишком большой ответ ловушки')
        return response.status, data, source_port
    finally:
        conn.close()


def resp(stream):
    line = stream.readline(8193)
    require(line.endswith(b'\r\n') and len(line) <= 8192, 'Некорректный RESP-ответ')
    prefix, value = line[:1], line[1:-2]
    if prefix == b'$':
        length = int(value)
        require(-1 <= length <= 8192, 'RESP bulk превышает лимит')
        if length == -1:
            return None
        data = stream.read(length + 2)
        require(len(data) == length + 2 and data.endswith(b'\r\n'), 'Неполный RESP bulk')
        return data[:-2]
    require(prefix in (b'+', b'-', b':'), 'Неожиданный RESP-тип')
    return prefix + value


def redis_probe(host, port, password, marker):
    with socket.create_connection((host, port), timeout=15) as conn:
        source_port = conn.getsockname()[1]
        with conn.makefile('rb') as stream:
            def command(*args):
                parts = [str(a).encode() for a in args]
                conn.sendall(b'*%d\r\n' % len(parts) + b''.join(b'$%d\r\n' % len(p) + p + b'\r\n' for p in parts))
                return resp(stream)
            require(command('AUTH', password) == b'+OK', 'Redis AUTH не принят')
            require(command('PING') == b'+PONG', 'Redis не ответил PONG')
            require(command('SET', 'jury:demo', marker) == b'+OK', 'Redis SET не принят')
            require(command('GET', 'jury:demo') == marker.encode(), 'Redis GET потерял значение')
            print('  AUTH → OK; PING → PONG; SET/GET →', marker)
        return source_port


def ssh_probe(host, port, password):
    import paramiko
    with socket.create_connection((host, port), timeout=15) as conn:
        source_port = conn.getsockname()[1]
        # New disposable decoy; never use the operator password or persist host keys.
        with paramiko.Transport(conn) as transport:
            transport.start_client(timeout=15)
            print('  SSH host key:', transport.get_remote_server_key().get_fingerprint().hex())
            transport.auth_password('analyst', password)
            with transport.open_session(timeout=15) as channel:
                channel.settimeout(15)
                channel.exec_command('whoami; pwd; uname -a; ls -la')
                output = bytearray()
                deadline = time.monotonic() + 20
                while time.monotonic() < deadline:
                    if channel.recv_ready():
                        output.extend(channel.recv(4096))
                        require(len(output) <= 16384, 'SSH output превышает лимит')
                    elif channel.exit_status_ready():
                        break
                    else:
                        time.sleep(0.05)
                else:
                    raise TimeoutError('SSH-команда не завершилась')
                require(bool(output), 'SSH-команда не вернула ответ')
                require(channel.recv_exit_status() == 0, 'SSH-команда завершилась с ошибкой')
                print('  ', output.decode(errors='replace').strip())
        return source_port


def probe(item, host, password, marker):
    key, port = item['key'], item['port']
    if key == 'redis':
        return redis_probe(host, port, password, marker)
    if key == 'ssh':
        return ssh_probe(host, port, password)
    if key == 'http':
        status, data, source = http_probe(host, port, '/?jury=' + marker, method='GET')
        require(200 <= status < 500, f'Dionaea HTTP: {status}')
        print('  HTTP', status, data[:150].decode(errors='replace'))
        return source
    with socket.create_connection((host, port), timeout=15) as conn:
        source = conn.getsockname()[1]
        if key in ('mail', 'console'):
            banner = conn.recv(4096)
            require(bool(banner), 'Пустой TCP-баннер')
            conn.sendall(('EHLO jury.local\r\n' + marker + '\r\nQUIT\r\n').encode())
            print('  Баннер:', banner.decode(errors='replace').strip(), '| отправлен', marker)
            return source
        requests = {
            'modbus': bytes.fromhex('000100000006010100010001'),
            's7comm': bytes.fromhex('0300001611e00000000600c1020100c2020102c0010a'),
            'memcache': b'get missing-key\r\n',
        }
        conn.sendall(requests[key])
        data = conn.recv(4096)
        require(bool(data), key + ': пустой ответ')
        if key == 'modbus':
            require(len(data) >= 9 and data[:2] == b'\x00\x01', 'Некорректный Modbus transaction ID')
        elif key == 's7comm':
            require(data.startswith(b'\x03\x00'), 'Некорректный S7 TPKT')
        else:
            require(data == b'END\r\n', 'Memcached: ожидался END')
        print('  Запрос:', requests[key].hex(), '→ ответ:', data.hex())
        return source
