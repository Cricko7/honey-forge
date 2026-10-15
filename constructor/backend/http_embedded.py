"""Validate static bait configuration before deploying an embedded token trap."""
import posixpath
import re
from fastapi import HTTPException


def validate_tokens(tokens):
    invalid = HTTPException(422, 'Нужны 1–32 приманки с уникальными id и путями; key — ключ длиной 16–128 символов')
    if not isinstance(tokens, list) or not 1 <= len(tokens) <= 32:
        raise invalid
    ids, paths, keys = set(), set(), set()
    for token in tokens:
        if not isinstance(token, dict) or set(token) != {'id', 'kind', 'path', 'value'} or any(not isinstance(v, str) for v in token.values()):
            raise invalid
        ident, kind, path, value = (token[k] for k in ('id', 'kind', 'path', 'value'))
        if not re.fullmatch(r'[a-z][a-z0-9_-]{0,31}', ident) or kind not in ('url', 'file', 'key') or not 2 <= len(path) <= 256 or not re.fullmatch(r'/[a-zA-Z0-9/_-]+(?:\.[a-zA-Z0-9_-]+)?', path) or posixpath.normpath(path) != path or path == '/artifacts' or path.startswith('/artifacts/') or not value or len(value.encode()) > 4096 or ident in ids or path in paths:
            raise invalid
        if kind == 'key' and (not 16 <= len(value) <= 128 or any(c.isspace() for c in value) or value in keys):
            raise invalid
        ids.add(ident)
        paths.add(path)
        if kind == 'key':
            keys.add(value)
