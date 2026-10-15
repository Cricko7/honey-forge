"""Strict JSON boundary shared by all constructor mutations."""
import json
import math

from fastapi import HTTPException


def check_values(value):
    if isinstance(value, str):
        if any(0xD800 <= ord(char) <= 0xDFFF for char in value):
            raise ValueError('Invalid Unicode string')
    elif isinstance(value, float) and not math.isfinite(value):
        raise ValueError('Finite numbers required')
    elif isinstance(value, dict):
        for key, item in value.items():
            check_values(key)
            check_values(item)
    elif isinstance(value, list):
        for item in value:
            check_values(item)


def check_document(body):
    def object_pairs(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError('Duplicate JSON key')
            result[key] = value
        return result

    def constant(_):
        raise ValueError('Non-JSON constant')

    try:
        value = json.loads(body.decode('utf-8'), object_pairs_hook=object_pairs, parse_constant=constant)
    except (ValueError, UnicodeError, RecursionError) as error:
        raise HTTPException(400, 'Invalid JSON document') from error
    try:
        check_values(value)
    except (ValueError, RecursionError) as error:
        raise HTTPException(422, 'Invalid JSON values') from error
