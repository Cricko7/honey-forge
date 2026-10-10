// Типы DTO — 1:1 с api/openapi.yaml (HoneyForge API 0.1.0).
// snake_case сохраняем специально: это формат обмена с backend.

export type UUID = string
export type Timestamp = string // RFC 3339, UTC Z

export type Role = 'admin' | 'viewer'
export type Connectivity = 'online' | 'offline'
export type RuntimeState = 'unknown' | 'running' | 'stopped' | 'error'
export type DesiredState = 'running' | 'stopped'
export type CommandAction = 'apply_config' | 'start' | 'stop'
export type CommandStatus = 'queued' | 'running' | 'succeeded' | 'failed' | 'expired'
export type BufferState = 'ok' | 'full' | 'unavailable'

export interface User {
  id: UUID
  email: string
  role: Role
}

export interface Organization {
  id: UUID
  name: string
  created_at: Timestamp
}

export interface SessionView {
  user: User
  organization: Organization
  expires_at: Timestamp
  csrf_token: string
}

export interface RuntimeError {
  code: string
  message: string
}

export interface AgentStatus {
  agent_version: string
  hostname: string
  buffered_events: number
  buffer_bytes: number
  buffer_capacity_bytes: number
  buffer_state: BufferState
  last_error: RuntimeError | null
}

export interface Trap {
  id: UUID
  name: string
  description: string
  profile_id: UUID
  type_id: string
  type_version: number
  interaction_level: string
  revision: number
  state_version: number
  created_at: Timestamp
  updated_at: Timestamp
  connectivity: Connectivity
  last_seen_at: Timestamp | null
  runtime_state: RuntimeState
  desired_state: DesiredState
  desired_profile_revision: number | null
  applied_profile_revision: number | null
  agent: AgentStatus | null
  active_command_id: UUID | null
}

export interface Profile {
  id: UUID
  name: string
  description: string
  type_id: string
  type_version: number
  interaction_level: string
  config: Record<string, unknown>
  secret_fields_set: string[]
  revision: number
  created_at: Timestamp
  updated_at: Timestamp
}

export interface EventSource {
  ip: string
  port: number
}
export interface EventDestination {
  protocol: string
  port: number
}

export interface EventSummary {
  event_id: UUID
  event_type: string
  type_id: string
  type_version: number
  profile_revision: number
  occurred_at: Timestamp
  session_id: UUID
  session_sequence: number
  source: EventSource
  destination: EventDestination
  trap_id: UUID
  received_at: Timestamp
}

export interface Event extends EventSummary {
  data: Record<string, unknown>
  source_enrichment: {
    country_code: string | null
    asn: number | null
  }
}

export interface Command {
  id: UUID
  trap_id: UUID
  request_id: UUID
  action: CommandAction
  params: Record<string, unknown>
  status: CommandStatus
  target_profile_revision: number | null
  created_at: Timestamp
  expires_at: Timestamp
  started_at: Timestamp | null
  finished_at: Timestamp | null
  result: Record<string, unknown> | null
  error: RuntimeError | null
}

export interface EventDescriptor {
  event_type: string
  title: string
  data_schema: Record<string, unknown>
}
export interface ActionDescriptor {
  action: string
  title: string
  params_schema: Record<string, unknown>
  result_schema: Record<string, unknown>
}
export interface CatalogEntry {
  type_id: string
  type_version: number
  title: string
  description: string
  interaction_level: string
  available_for_new_profiles: boolean
  config_schema: Record<string, unknown>
  event_schemas: EventDescriptor[]
  actions: ActionDescriptor[]
  ui: {
    field_order: string[]
    widgets: Record<string, string>
  }
}

export interface Page<T> {
  items: T[]
  next_cursor: string | null
}

export interface EventPage extends Page<EventSummary> {
  stream_cursor: string
}

// WSS /api/stream — уведомления дашборда
export type StreamNotification =
  | { kind: 'event.created'; event: EventSummary }
  | { kind: 'trap.changed'; trap: Trap }
  | { kind: 'trap.deleted'; trap_id: UUID }
  | { kind: 'command.changed'; trap_id: UUID; command: Command }

// Позиции узлов на canvas-конструкторе. Чисто фронтовое наложение
// (бэкенд не хранит раскладку) — persist в localStorage.
export interface NodeLayout {
  x: number
  y: number
}
export type NetworkLayout = Record<UUID, NodeLayout>
