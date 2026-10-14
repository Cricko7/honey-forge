"""Constructor on the panel origin, using HoneyForge session and authorization."""
import asyncio
import hmac
import os
from pathlib import Path
from typing import Literal
import uuid

from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import FileResponse, JSONResponse, StreamingResponse
from fastapi.staticfiles import StaticFiles
from fastapi.exceptions import RequestValidationError
from pydantic import BaseModel, ConfigDict

from . import real_api
from .custom import registry_types
from .models import ChainCreate, ChainUpdate
from .node_registry import registry_payload
from .runtime import runtime
from .store import organization, store

STATIC_DIR = Path(__file__).resolve().parent.parent / 'static'
app = FastAPI(title='HoneyForge Constructor', version='1.0.0')


@app.exception_handler(real_api.RealApiError)
async def backend_error(request, error):
    return JSONResponse({'error': {'code': error.code, 'message': str(error)}}, status_code=error.status)


@app.exception_handler(HTTPException)
async def request_error(request, error):
    return JSONResponse({'error': {'code': 'constructor_error', 'message': error.detail}}, status_code=error.status_code)


@app.exception_handler(RequestValidationError)
async def validation_error(request, error):
    malformed = any(issue['type'] == 'json_invalid' for issue in error.errors())
    return JSONResponse({'error': {'code': 'invalid_json' if malformed else 'validation_failed', 'message': 'Invalid constructor request'}}, status_code=400 if malformed else 422)


@app.middleware('http')
async def operator_session(request: Request, call_next):
    path = request.scope['path']
    root = request.scope.get('root_path', '')
    if root and path.startswith(root + '/'):
        path = path[len(root):]
    if not path.startswith('/api/'):
        return await call_next(request)
    token = real_api.request_headers.set({key: request.headers[key] for key in ('Cookie', 'X-CSRF-Token', 'Origin') if key in request.headers})
    org_token = None
    try:
        session = await asyncio.to_thread(real_api._request, 'GET', '/api/session')
        org_token = organization.set(str(uuid.UUID(session['organization']['id'])))
        if request.method != 'GET':
            if session['user']['role'] != 'admin':
                raise HTTPException(403, 'Доступно только администратору')
            if request.headers.get('origin') != os.environ.get('HONEYFORGE_ORIGIN', 'https://localhost:8443') or not hmac.compare_digest(request.headers.get('x-csrf-token', ''), session['csrf_token']):
                raise HTTPException(403, 'Invalid CSRF token or origin')
        return await call_next(request)
    except real_api.RealApiError as error:
        return await backend_error(request, error)
    except HTTPException as error:
        return await request_error(request, error)
    finally:
        if org_token is not None:
            organization.reset(org_token)
        real_api.request_headers.reset(token)


def chain_or_404(chain_id):
    chain = store.get(chain_id)
    if not chain:
        raise HTTPException(404, 'Цепочка не найдена')
    return chain


def public_chain(chain):
    data = chain.model_dump(exclude={'bindings'})
    for node in data['nodes']:
        node['config'].pop('password', None)
    return data


@app.get('/api/registry')
def registry():
    data = registry_payload()
    return {'categories': [{'id': 'http', 'label': 'Собрать HTTP-ловушку', 'accent': '#2c9b80'}, {'id': 'custom', 'label': 'Собрать TCP-ловушку', 'accent': '#4A6CF7'}, *data['categories']],
            'node_types': [*registry_types(), *data['node_types']]}


@app.get('/api/templates')
def templates():
    return []


@app.get('/api/chains')
def chains():
    return store.list()


@app.post('/api/chains', status_code=201)
def create(data: ChainCreate):
    chain = store.create(data)
    return JSONResponse(public_chain(chain), status_code=201, headers={'Location': '/constructor/api/chains/' + chain.id})


@app.get('/api/chains/{chain_id}')
def read(chain_id: str):
    return public_chain(chain_or_404(chain_id))


@app.patch('/api/chains/{chain_id}')
def update(chain_id: str, data: ChainUpdate):
    chain_or_404(chain_id)
    if data.nodes is not None and len({n.id for n in data.nodes}) != len(data.nodes):
        raise HTTPException(422, 'Идентификаторы блоков должны быть уникальными')
    with runtime.lock:
        return public_chain(store.update(chain_id, data))


@app.delete('/api/chains/{chain_id}', status_code=204)
def delete(chain_id: str):
    chain_or_404(chain_id)
    store.delete(chain_id)


@app.post('/api/chains/{chain_id}/duplicate', status_code=201)
def duplicate(chain_id: str):
    chain_or_404(chain_id)
    return public_chain(store.duplicate(chain_id))


@app.get('/api/chains/{chain_id}/runtime')
def status(chain_id: str):
    chain_or_404(chain_id)
    return runtime.status(chain_id)


class DeployRequest(BaseModel):
    model_config = ConfigDict(extra='forbid')
    action: Literal['deploy', 'stop'] = 'deploy'


@app.post('/api/chains/{chain_id}/deploy')
def deploy(chain_id: str, data: DeployRequest):
    chain_or_404(chain_id)
    return runtime.deploy(chain_id, data.action)


@app.get('/api/chains/{chain_id}/stream')
async def stream(chain_id: str):
    chain_or_404(chain_id)
    return StreamingResponse(runtime.stream(chain_id), media_type='text/event-stream', headers={'Cache-Control': 'no-cache', 'X-Accel-Buffering': 'no'})


@app.get('/api/real/catalog')
def catalog():
    return {'online': True, 'items': real_api.list_trap_types()}


@app.get('/api/real/traps')
def traps():
    return {'online': True, 'items': real_api.list_traps()}


class RealCommand(BaseModel):
    model_config = ConfigDict(extra='forbid')
    action: Literal['start', 'stop']


@app.get('/api/real/profiles')
def profiles():
    return {'online': True, 'items': real_api.list_all('/api/profiles')}


class RealTrapCreate(BaseModel):
    model_config = ConfigDict(extra='forbid')
    request_id: uuid.UUID
    name: str
    profile_id: uuid.UUID
    description: str = ''


@app.post('/api/real/traps', status_code=201)
def create_profile_trap(data: RealTrapCreate):
    trap = real_api._request('POST', '/api/traps', data.model_dump(mode='json'))
    return {'ok': True, 'trap': trap}


@app.post('/api/real/traps/{trap_id}/command')
def command(trap_id: uuid.UUID, data: RealCommand):
    return {'ok': True, 'command': real_api.send_command(str(trap_id), data.action)}


@app.get('/')
def index():
    return FileResponse(STATIC_DIR / 'index.html')


app.mount('/', StaticFiles(directory=STATIC_DIR), name='static')
