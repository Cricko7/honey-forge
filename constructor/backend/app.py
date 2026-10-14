"""FastAPI-приложение конструктора цепочек ловушек HoneyForge.

Отдаёт REST API (реестр узлов, шаблоны, CRUD цепочек, валидация, симуляция) и
статический фронтенд с бесконечной доской. Полностью автономно, данные fake.

Запуск:
    uvicorn honeyforge.constructor.backend.app:app --reload --port 8077
или:
    python -m honeyforge.constructor.backend.app
"""
from __future__ import annotations

from pathlib import Path

import random

from fastapi import FastAPI, HTTPException
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import FileResponse, StreamingResponse
from fastapi.staticfiles import StaticFiles
from pydantic import BaseModel

from . import real_api
from .models import Chain, ChainCreate, ChainUpdate, SimulateRequest, ValidationResult
from .node_registry import registry_payload
from .runtime import runtime
from .simulator import simulate
from .store import store, templates_payload
from .validation import validate_chain

STATIC_DIR = Path(__file__).resolve().parent.parent / "static"

app = FastAPI(title="HoneyForge Chain Constructor", version="0.1.0")
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"], allow_methods=["*"], allow_headers=["*"],
)

api = app  # алиас


# ---- мета ----
@app.get("/api/registry")
def get_registry() -> dict:
    return registry_payload()


@app.get("/api/templates")
def get_templates() -> list[dict]:
    return templates_payload()


# ---- цепочки ----
@app.get("/api/chains")
def list_chains() -> list:
    return store.list()


@app.post("/api/chains", status_code=201)
def create_chain(data: ChainCreate) -> Chain:
    return store.create(data)


@app.get("/api/chains/{chain_id}")
def get_chain(chain_id: str) -> Chain:
    c = store.get(chain_id)
    if not c:
        raise HTTPException(404, "Цепочка не найдена")
    return c


@app.patch("/api/chains/{chain_id}")
def update_chain(chain_id: str, data: ChainUpdate) -> Chain:
    c = store.update(chain_id, data)
    if not c:
        raise HTTPException(404, "Цепочка не найдена")
    return c


@app.delete("/api/chains/{chain_id}", status_code=204)
def delete_chain(chain_id: str) -> None:
    if not store.delete(chain_id):
        raise HTTPException(404, "Цепочка не найдена")


@app.post("/api/chains/{chain_id}/duplicate", status_code=201)
def duplicate_chain(chain_id: str) -> Chain:
    c = store.duplicate(chain_id)
    if not c:
        raise HTTPException(404, "Цепочка не найдена")
    return c


# ---- валидация / симуляция ----
@app.post("/api/chains/{chain_id}/validate")
def validate(chain_id: str) -> ValidationResult:
    c = store.get(chain_id)
    if not c:
        raise HTTPException(404, "Цепочка не найдена")
    return validate_chain(c)


@app.post("/api/chains/{chain_id}/simulate")
def run_simulation(chain_id: str, req: SimulateRequest) -> dict:
    c = store.get(chain_id)
    if not c:
        raise HTTPException(404, "Цепочка не найдена")
    return simulate(c, req)


# ---- рантайм ловушек / живой поток ----
class DeployRequest(BaseModel):
    action: str = "deploy"   # deploy | stop


@app.get("/api/chains/{chain_id}/runtime")
def get_runtime(chain_id: str) -> dict:
    if not store.get(chain_id):
        raise HTTPException(404, "Цепочка не найдена")
    return runtime.status(chain_id)


@app.post("/api/chains/{chain_id}/deploy")
def deploy_chain(chain_id: str, req: DeployRequest) -> dict:
    if not store.get(chain_id):
        raise HTTPException(404, "Цепочка не найдена")
    return runtime.deploy(chain_id, req.action)


@app.get("/api/chains/{chain_id}/stream")
async def stream_chain(chain_id: str) -> StreamingResponse:
    if not store.get(chain_id):
        raise HTTPException(404, "Цепочка не найдена")
    gen = runtime.stream(chain_id, random.Random())
    return StreamingResponse(gen, media_type="text/event-stream", headers={
        "Cache-Control": "no-cache", "X-Accel-Buffering": "no", "Connection": "keep-alive",
    })


# ---- интеграция с РЕАЛЬНЫМ бэкендом HoneyForge (прокси, обход CORS) ----
@app.get("/api/real/health")
def real_health() -> dict:
    return {"online": real_api.health(), "api_base": real_api.API_BASE}


@app.get("/api/real/catalog")
def real_catalog() -> dict:
    try:
        return {"online": True, "items": real_api.list_trap_types()}
    except real_api.RealApiError as e:
        return {"online": False, "items": [], "error": str(e)}


@app.get("/api/real/traps")
def real_traps() -> dict:
    try:
        return {"online": True, "items": real_api.list_traps()}
    except real_api.RealApiError as e:
        return {"online": False, "items": [], "error": str(e)}


class RealCommand(BaseModel):
    action: str  # start | stop | apply_config


@app.post("/api/real/traps/{trap_id}/command")
def real_command(trap_id: str, req: RealCommand) -> dict:
    try:
        cmd = real_api.send_command(trap_id, req.action)
        return {"ok": True, "command": cmd}
    except real_api.RealApiError as e:
        raise HTTPException(502, f"Бэкенд отклонил команду: {e}")


# ---- статика (SPA) ----
if STATIC_DIR.exists():
    app.mount("/assets", StaticFiles(directory=STATIC_DIR / "assets"), name="assets") \
        if (STATIC_DIR / "assets").exists() else None

    @app.get("/")
    def index() -> FileResponse:
        return FileResponse(STATIC_DIR / "index.html")

    app.mount("/", StaticFiles(directory=STATIC_DIR, html=True), name="static")


def main() -> None:
    import uvicorn
    uvicorn.run("honeyforge.constructor.backend.app:app", host="127.0.0.1", port=8077, reload=False)


if __name__ == "__main__":
    main()
