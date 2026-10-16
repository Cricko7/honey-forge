"""Operator session stays in memory; listener traffic never uses this client."""
import getpass
import http.client
from http.cookies import SimpleCookie
import json
import ssl
import time
from urllib.parse import urlsplit, urlencode


def origin(value):
    url = urlsplit(value.strip())
    if (url.scheme != 'https' or not url.hostname or url.username or url.password
            or url.query or url.fragment):
        raise ValueError('Нужен HTTPS-адрес сайта без пароля, query и fragment')
    return f'https://{url.netloc}'


class ApiError(RuntimeError):
    def __init__(self, status, code):
        self.status, self.code = status, code
        super().__init__(f'API HTTP {status}: {code}')


class Client:
    def __init__(self, base, ca=None):
        self.base = origin(base)
        self.url = urlsplit(self.base)
        self.context = ssl.create_default_context(cafile=ca)
        self.cookies, self.csrf = {}, None

    def request(self, method, path, body=None):
        attempts = 3 if method == 'GET' else 1
        for attempt in range(attempts):
            try:
                return self._request(method, path, body)
            except (ssl.SSLEOFError, http.client.RemoteDisconnected, ConnectionResetError):
                if attempt + 1 == attempts:
                    raise

    def _request(self, method, path, body=None):
        if not path.startswith('/') or path.startswith('//'):
            raise ValueError('API path must be relative')
        # Retry only TLS setup, before any HTTP bytes could have been sent.
        for attempt in range(3):
            conn = http.client.HTTPSConnection(self.url.hostname, self.url.port,
                                               timeout=60, context=self.context)
            try:
                conn.connect()
                break
            except ssl.SSLEOFError:
                conn.close()
                if attempt == 2:
                    raise
            except BaseException:
                conn.close()
                raise
        try:
            headers = {'Origin': self.base, 'Content-Type': 'application/json',
                       'Cookie': self.cookie_header()}
            if self.csrf:
                headers['X-CSRF-Token'] = self.csrf
            conn.request(method, path, json.dumps(body).encode() if body is not None else None, headers)
            response = conn.getresponse()
            for key, value in response.getheaders():
                if key.lower() == 'set-cookie':
                    cookie = SimpleCookie()
                    cookie.load(value)
                    self.cookies.update({k: v.value for k, v in cookie.items()})
            raw = response.read(4 * 1024 * 1024 + 1)
            if len(raw) > 4 * 1024 * 1024:
                raise RuntimeError('API response exceeds 4 MiB')
            if response.status >= 300:
                try:
                    code = json.loads(raw).get('error', {}).get('code', 'request_failed')
                except (ValueError, AttributeError):
                    code = 'non_json_response'
                raise ApiError(response.status, code)
            return json.loads(raw) if raw else None
        finally:
            conn.close()

    def cookie_header(self):
        return '; '.join(k + '=' + v for k, v in self.cookies.items())

    def login(self):
        login = input('Login: ').strip()
        password = getpass.getpass('Password: ')
        session = self.request('POST', '/api/sessions', {'email': login, 'password': password})
        self.csrf = session['csrf_token']
        current = self.request('GET', '/api/session')
        user = current['user']
        if user['role'] != 'admin':
            raise RuntimeError('Для связей сети нужна учётная запись администратора')
        print('Вход выполнен. Организация:', current['organization']['name'], flush=True)

    def items(self, path):
        cursor = None
        while True:
            query = {'limit': 100}
            if cursor:
                query['cursor'] = cursor
            page = self.request('GET', path + '?' + urlencode(query))
            yield from page['items']
            cursor = page.get('next_cursor')
            if not cursor:
                return


def wait_for(function, description, timeout=180):
    deadline = time.monotonic() + timeout
    next_notice = time.monotonic() + 15
    while True:
        result = function()
        if result:
            return result
        if time.monotonic() >= deadline:
            raise TimeoutError(description)
        if time.monotonic() >= next_notice:
            print('Ожидание:', description, flush=True)
            next_notice = time.monotonic() + 15
        time.sleep(1)
