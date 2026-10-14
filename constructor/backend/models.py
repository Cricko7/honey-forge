"""Pydantic-модели конструктора цепочек ловушек."""
from __future__ import annotations

import time
import uuid
from typing import Any

from pydantic import BaseModel, ConfigDict, Field


def _id(prefix: str) -> str:
    return f"{prefix}_{uuid.uuid4().hex[:10]}"


class Position(BaseModel):
    x: float = 0
    y: float = 0


class Node(BaseModel):
    id: str = Field(default_factory=lambda: _id("node"), pattern=r'^[a-zA-Z0-9_-]{1,64}$')
    type: str                      # ключ из node_registry
    position: Position = Field(default_factory=Position)
    label: str | None = None       # пользовательское имя узла
    config: dict[str, Any] = Field(default_factory=dict)
    notes: str = ""


class Edge(BaseModel):
    id: str = Field(default_factory=lambda: _id("edge"), pattern=r'^[a-zA-Z0-9_-]{1,64}$')
    source: str = Field(pattern=r'^[a-zA-Z0-9_-]{1,64}$')
    source_port: str               # port id
    target: str = Field(pattern=r'^[a-zA-Z0-9_-]{1,64}$')
    target_port: str
    label: str = ""                # подпись на ребре (условие перехода)


class Viewport(BaseModel):
    x: float = 0
    y: float = 0
    zoom: float = 1.0


class ChainMeta(BaseModel):
    id: str = Field(default_factory=lambda: _id("chain"))
    name: str = "Новая цепочка"
    description: str = ""
    tags: list[str] = Field(default_factory=list)
    created_at: float = Field(default_factory=time.time)
    updated_at: float = Field(default_factory=time.time)


class Chain(ChainMeta):
    bindings: dict[str, dict[str, str]] = Field(default_factory=dict)
    nodes: list[Node] = Field(default_factory=list)
    edges: list[Edge] = Field(default_factory=list)
    viewport: Viewport = Field(default_factory=Viewport)


class ChainSummary(ChainMeta):
    node_count: int = 0
    edge_count: int = 0


# ---- входные DTO ----
class ChainCreate(BaseModel):
    model_config = ConfigDict(extra='forbid')
    name: str = Field(default='Новая цепочка', min_length=1, max_length=100)
    description: str = ""
    tags: list[str] = Field(default_factory=list)
    template: str | None = None    # id встроенного шаблона


class ChainUpdate(BaseModel):
    model_config = ConfigDict(extra='forbid')
    name: str | None = Field(default=None, min_length=1, max_length=100)
    description: str | None = None
    tags: list[str] | None = None
    nodes: list[Node] | None = Field(default=None, max_length=512)
    edges: list[Edge] | None = Field(default=None, max_length=1024)
    viewport: Viewport | None = None


class SimulateRequest(BaseModel):
    entry_node_id: str | None = None   # откуда стартует атакующий; None = авто
    attacker_profile: str = "opportunistic"  # opportunistic | targeted | automated
    seed: int | None = None


class ValidationIssue(BaseModel):
    level: str          # error | warning | info
    node_id: str | None = None
    edge_id: str | None = None
    message: str


class ValidationResult(BaseModel):
    ok: bool
    issues: list[ValidationIssue] = Field(default_factory=list)
