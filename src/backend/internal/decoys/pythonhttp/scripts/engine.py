"""Bounded, request-isolated HTTP flow interpreter. No eval or outgoing sockets."""
import asyncio
import copy
import fnmatch
import json
import re
import time
from urllib.parse import parse_qs


class FlowError(ValueError):
    pass


def lookup(data, selector):
    value = data
    for part in selector.split('.'):
        if isinstance(value, dict):
            value = value.get(part)
        elif isinstance(value, list) and part.isdecimal():
            index = int(part)
            value = value[index] if index < len(value) else None
        else:
            return None
    return value


def text(value):
    if isinstance(value, str):
        return value
    return json.dumps(value, ensure_ascii=False, allow_nan=False, separators=(',', ':'))


def template(value, data):
    if isinstance(value, dict):
        return {k: template(v, data) for k, v in value.items()}
    if isinstance(value, list):
        return [template(v, data) for v in value]
    if not isinstance(value, str):
        return value
    exact = re.fullmatch(r'\$\{([a-zA-Z0-9_.-]+)\}', value)
    if exact:
        return copy.deepcopy(lookup(data, exact[1]))
    return re.sub(r'\$\{([a-zA-Z0-9_.-]+)\}', lambda m: text(lookup(data, m[1])), value)


def json_value(value, data):
    return template(json.loads(value), data)


def safe_headers(value):
    if not isinstance(value, dict) or len(value) > 64:
        raise FlowError('invalid response headers')
    for key, item in value.items():
        if not isinstance(key, str) or not re.fullmatch(r'[!#$%&\x27*+.^_`|~0-9a-zA-Z-]+', key) or not isinstance(item, str):
            raise FlowError('invalid response header')
        if key.lower() in ('connection', 'content-length', 'transfer-encoding') or '\r' in item or '\n' in item or len(item.encode()) > 8192:
            raise FlowError('unsafe response header')
        try:
            item.encode('latin-1')
        except UnicodeEncodeError as error:
            raise FlowError('header must use latin-1') from error
    return value


class Engine:
    def __init__(self, blocks, max_steps=512, timeout_ms=10000, max_body_bytes=65536):
        self.blocks = {b['id']: b for b in blocks}
        self.max_steps = max_steps
        self.timeout = timeout_ms / 1000
        self.max_body = max_body_bytes
        self.sessions = {}  # accessed only on the server's asyncio loop

    async def execute(self, entry, request, emit):
        data = {'json': None, 'vars': {}, 'body': '', 'headers': {}, 'query': {}, 'cookies': {}, **copy.deepcopy(request)}
        async with asyncio.timeout(self.timeout):
            return await self.walk(entry, data, emit, [0], 0)

    async def walk(self, entry, data, emit, budget, depth):
        if depth > 32:
            raise FlowError('call depth exceeded')
        current = entry
        while current:
            if budget[0] >= self.max_steps:
                raise FlowError('step limit exceeded')
            budget[0] += 1
            block = self.blocks.get(current)
            if block is None:
                raise FlowError('missing flow block')
            kind, config, outputs = block['kind'], block['config'], block['outputs']
            await emit('http.block', {'node_id': current, 'kind': kind, 'phase': 'started', 'step': budget[0]})
            try:
                result, branch = await self.action(kind, config, outputs, data, emit, budget, depth)
                # Bound intermediate expansion too, before the next block can reuse it.
                if len(text({'json': data['json'], 'vars': data['vars']}).encode()) > self.max_body:
                    raise FlowError('intermediate data limit exceeded')
                await emit('http.block', {'node_id': current, 'kind': kind, 'phase': 'completed', 'branch': branch, 'step': budget[0]})
                if result is not None:
                    return result
                current = outputs.get(branch)
                if not current:
                    raise FlowError('unconnected flow output')
            except (ValueError, TypeError, KeyError, OverflowError, RecursionError) as error:
                await emit('http.block', {'node_id': block['id'], 'kind': kind, 'phase': 'failed', 'message': str(error)[:256], 'step': budget[0]})
                if 'error' not in outputs:
                    raise FlowError(str(error)) from error
                data['error'] = str(error)[:256]
                current = outputs['error']
        raise FlowError('flow ended without response')

    async def action(self, kind, c, outputs, data, emit, budget, depth):
        branch = 'out'
        value = lambda key: json_value(c[key], data)
        if kind == 'input':
            pass  # method/path apply only at the external boundary, not to internal calls
        elif kind == 'parse':
            fmt = c['format']
            if fmt == 'auto':
                content_type = data['headers'].get('content-type', '')
                fmt = 'json' if 'json' in content_type else 'form' if 'x-www-form-urlencoded' in content_type else 'text'
            data['json'] = json.loads(data['body']) if fmt == 'json' else {k: v[-1] for k, v in parse_qs(data['body'], max_num_fields=256).items()} if fmt == 'form' else data['body']
        elif kind == 'extract':
            found = lookup(data, c['source'])
            if found is None and c['required']:
                raise FlowError('required field is absent')
            data['vars'][c['key']] = copy.deepcopy(found)
        elif kind == 'condition':
            found = lookup(data, c['source'])
            expected = template(c['value'], data)
            op = c['operator']
            if op == 'exists':
                matched = found is not None
            elif op in ('gt', 'lt'):
                matched = float(found) > float(expected) if op == 'gt' else float(found) < float(expected)
            elif op == 'contains':
                matched = text(expected) in text(found)
            elif op == 'glob':
                matched = fnmatch.fnmatchcase(text(found), text(expected))
            else:
                matched = text(found) == text(expected)
                if op == 'not_equals':
                    matched = not matched
            branch = 'yes' if matched else 'no'
        elif kind == 'switch':
            found = text(lookup(data, c['source']))
            branch = next(('case' + str(i) for i in range(1, 4) if found == text(template(c['value' + str(i)], data))), 'default')
        elif kind == 'transform':
            key, op = c['key'], c['operation']
            item = value('value')
            previous = data['vars'].get(key)
            if op == 'remove':
                data['vars'].pop(key, None)
            else:
                if op in ('upper', 'lower'):
                    item = text(item).upper() if op == 'upper' else text(item).lower()
                elif op == 'add':
                    item = (previous or 0) + item
                elif op == 'merge':
                    if not isinstance(item, dict) or previous is not None and not isinstance(previous, dict):
                        raise FlowError('merge requires JSON objects')
                    item = {**(previous or {}), **item}
                data['vars'][key] = item
        elif kind == 'state':
            session = data.get('session')
            if not session:
                raise FlowError('session is absent')
            now = time.monotonic()
            for key in [key for key, (at, _) in self.sessions.items() if now - at > 1800]:
                del self.sessions[key]
            if session not in self.sessions and len(self.sessions) >= 1024:
                raise FlowError('session capacity reached')
            _, state = self.sessions.setdefault(session, (now, {}))
            self.sessions[session] = (now, state)
            key, op = c['key'], c['operation']
            if op == 'set':
                updated = {**state, key: value('value')}
                if len(text(updated).encode()) > 16384:
                    raise FlowError('session data limit exceeded')
                state.update(updated)
            elif op == 'delete':
                state.pop(key, None)
            data['vars'][key] = copy.deepcopy(state.get(key))
        elif kind == 'call':
            payload = value('payload')
            child = copy.deepcopy(data)
            child['json'], child['body'] = payload, json.dumps(payload, ensure_ascii=False, allow_nan=False, separators=(',', ':'))
            result = await self.walk(c['target'], child, emit, budget, depth + 1)
            data['vars'][c['key']] = json.loads(result['body']) if result['format'] == 'json' else result['body']
            data['vars'][c['key'] + '_status'] = result['status']
        elif kind == 'fork':
            names = sorted(key for key in outputs if key.startswith('branch'))
            replies = await asyncio.gather(*[self.walk(outputs[name], copy.deepcopy(data), emit, budget, depth + 1) for name in names], return_exceptions=True)
            for reply in replies:
                if isinstance(reply, BaseException):
                    raise reply
            data['vars']['_branches'] = {name: {'status': reply['status'], 'body': json.loads(reply['body']) if reply['format'] == 'json' else reply['body']} for name, reply in zip(names, replies)}
        elif kind == 'join':
            if '_branches' not in data['vars']:
                raise FlowError('no branch results to join')
            data['vars'][c['key']] = data['vars'].pop('_branches')
        elif kind == 'delay':
            await asyncio.sleep(c['milliseconds'] / 1000)
        elif kind == 'error':
            data['vars'][c['key']] = data.get('error', 'unknown error')
        elif kind in ('response', 'redirect'):
            if kind == 'redirect':
                headers = safe_headers({'Location': text(template(c['url'], data))})
                return {'status': int(c['status']), 'body': '', 'headers': headers, 'format': 'text'}, 'response'
            fmt = c['format']
            body = json.dumps(value('body'), ensure_ascii=False, allow_nan=False, separators=(',', ':')) if fmt == 'json' else text(template(c['body'], data))
            if len(body.encode()) > self.max_body:
                raise FlowError('response body limit exceeded')
            headers = safe_headers(value('headers'))
            return {'status': c['status'], 'body': body, 'headers': headers, 'format': fmt}, 'response'
        else:
            raise FlowError('unknown block kind')
        return None, branch
