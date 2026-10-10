import type {
  Command,
  CommandAction,
  Event,
  EventSummary,
  Organization,
  Profile,
  SessionView,
  StreamNotification,
  Trap,
  User,
} from '@/types'
import { CATALOG } from './catalog'

// ---------------------------------------------------------------------------
// In-memory mock backend под контракт HoneyForge. Заменяется реальным API
// установкой VITE_API_BASE (см. client.ts). Данные демонстрационные.
// ---------------------------------------------------------------------------

const now = () => new Date().toISOString()
const minsAgo = (m: number) => new Date(Date.now() - m * 60_000).toISOString()

export function uuid(): string {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) return crypto.randomUUID()
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0
    const v = c === 'x' ? r : (r & 0x3) | 0x8
    return v.toString(16)
  })
}

const ORG: Organization = {
  id: uuid(),
  name: 'ООО «Периметр»',
  created_at: minsAgo(60 * 24 * 90),
}

const USER: User = {
  id: uuid(),
  email: 'admin@perimetr.ru',
  role: 'admin',
}

// --- Профили ---------------------------------------------------------------

function mkProfile(
  name: string,
  typeId: string,
  level: string,
  config: Record<string, unknown>,
  secretFields: string[] = [],
): Profile {
  return {
    id: uuid(),
    name,
    description: `Профиль ${name}`,
    type_id: typeId,
    type_version: 1,
    interaction_level: level,
    config,
    secret_fields_set: secretFields,
    revision: 1,
    created_at: minsAgo(60 * 24 * 10),
    updated_at: minsAgo(60 * 24 * 2),
  }
}

const profiles: Profile[] = [
  mkProfile('SSH-приманка', 'tcp-banner', 'low', {
    listeners: [{ name: 'ssh', port: 22, banner: 'SSH-2.0-OpenSSH_8.9', close_after_banner: false }],
    logging: { capture_payload: true, max_payload_bytes: 4096 },
    management: { heartbeat_interval_seconds: 5, telemetry_flush_interval_ms: 500 },
  }),
  mkProfile('FTP-приманка', 'tcp-banner', 'low', {
    listeners: [{ name: 'ftp', port: 21, banner: '220 ProFTPD Server ready', close_after_banner: false }],
    logging: { capture_payload: true, max_payload_bytes: 2048 },
    management: { heartbeat_interval_seconds: 5, telemetry_flush_interval_ms: 500 },
  }),
  mkProfile(
    'Redis-эмулятор',
    'redis-emulator',
    'medium',
    {
      services: [{ name: 'redis', port: 6379 }],
      management: { heartbeat_interval_seconds: 5, telemetry_flush_interval_ms: 500 },
    },
    ['/services/0/password'],
  ),
]

// --- Ловушки ---------------------------------------------------------------

function mkTrap(
  name: string,
  profile: Profile,
  conn: Trap['connectivity'],
  runtime: Trap['runtime_state'],
  hostname: string,
): Trap {
  return {
    id: uuid(),
    name,
    description: `Ловушка ${name}`,
    profile_id: profile.id,
    type_id: profile.type_id,
    type_version: profile.type_version,
    interaction_level: profile.interaction_level,
    revision: 1,
    state_version: 3,
    created_at: minsAgo(60 * 24 * 8),
    updated_at: minsAgo(30),
    connectivity: conn,
    last_seen_at: conn === 'online' ? minsAgo(1) : minsAgo(240),
    runtime_state: runtime,
    desired_state: runtime === 'running' ? 'running' : 'stopped',
    desired_profile_revision: profile.revision,
    applied_profile_revision: runtime === 'running' ? profile.revision : null,
    agent:
      conn === 'online'
        ? {
            agent_version: '1.0.49',
            hostname,
            buffered_events: Math.floor(Math.random() * 5),
            buffer_bytes: 2048,
            buffer_capacity_bytes: 1048576,
            buffer_state: 'ok',
            last_error: null,
          }
        : null,
    active_command_id: null,
  }
}

const traps: Trap[] = [
  mkTrap('dmz-ssh-01', profiles[0], 'online', 'running', 'dmz-host-01'),
  mkTrap('dmz-ftp-01', profiles[1], 'online', 'running', 'dmz-host-02'),
  mkTrap('lan-redis-01', profiles[2], 'online', 'running', 'lan-host-07'),
  mkTrap('lan-ssh-02', profiles[0], 'offline', 'stopped', 'lan-host-09'),
  mkTrap('edge-ftp-02', profiles[1], 'online', 'error', 'edge-host-03'),
]

// --- События (лог атак) ----------------------------------------------------

const ATTACKER_IPS = [
  { ip: '185.220.101.44', cc: 'RU', asn: 205100 },
  { ip: '45.155.205.233', cc: 'NL', asn: 59711 },
  { ip: '193.187.129.12', cc: 'CN', asn: 4134 },
  { ip: '103.251.167.20', cc: 'IN', asn: 133296 },
  { ip: '2.56.59.123', cc: 'DE', asn: 51167 },
  { ip: '141.98.11.9', cc: 'LT', asn: 209605 },
]

const CREDS = [
  { u: 'root', p: '123456' },
  { u: 'admin', p: 'admin' },
  { u: 'root', p: 'toor' },
  { u: 'oracle', p: 'oracle' },
  { u: 'user', p: 'password' },
  { u: 'root', p: 'P@ssw0rd' },
]

const REDIS_CMDS = ['AUTH', 'CONFIG GET *', 'KEYS *', 'INFO', 'SET backup1 ...', 'FLUSHALL', 'SLAVEOF']

const events: Event[] = []

function pickTrap(): Trap {
  const online = traps.filter((t) => t.connectivity === 'online')
  return online[Math.floor(Math.random() * online.length)]
}

function genEvent(trap: Trap, occurredAt: string): Event {
  const atk = ATTACKER_IPS[Math.floor(Math.random() * ATTACKER_IPS.length)]
  const isRedis = trap.type_id === 'redis-emulator'
  let eventType: string
  let data: Record<string, unknown>
  let destPort: number
  let proto: string

  if (trap.type_id === 'honeytoken-http') {
    const profile = profiles.find((p) => p.id === trap.profile_id)
    const service = (profile?.config.services as { name: string; port: number }[] | undefined)?.[0]
    const token = (profile?.config.tokens as { id: string; kind: string }[] | undefined)?.[0]
    destPort = service?.port ?? 8080
    proto = 'tcp'
    eventType = 'honeytoken.triggered'
    data = { service: service?.name ?? 'web', token_id: token?.id ?? 'backup-key', kind: token?.kind ?? 'key', method: 'GET' }
  } else if (isRedis) {
    const roll = Math.random()
    destPort = 6379
    proto = 'redis'
    if (roll < 0.45) {
      const c = CREDS[Math.floor(Math.random() * CREDS.length)]
      eventType = 'service.auth_attempt'
      data = { username: c.u, password: c.p, outcome: 'rejected', received_bytes: 48 }
    } else if (roll < 0.85) {
      eventType = 'service.action'
      data = { input: REDIS_CMDS[Math.floor(Math.random() * REDIS_CMDS.length)], received_bytes: 32 }
    } else {
      eventType = 'service.connection_opened'
      data = {}
    }
  } else {
    destPort = trap.name.includes('ftp') ? 21 : 22
    proto = trap.name.includes('ftp') ? 'ftp' : 'ssh'
    const roll = Math.random()
    if (roll < 0.5) {
      eventType = 'tcp.payload_received'
      const c = CREDS[Math.floor(Math.random() * CREDS.length)]
      const payload = `${c.u}:${c.p}`
      data = {
        listener_name: proto,
        payload_base64: btoa(payload),
        captured_bytes: payload.length,
        original_bytes: payload.length,
        truncated: false,
      }
    } else if (roll < 0.8) {
      eventType = 'tcp.connection_opened'
      data = { listener_name: proto }
    } else {
      eventType = 'tcp.connection_closed'
      data = { listener_name: proto, duration_ms: 1200, bytes_received: 64, reason: 'peer_closed' }
    }
  }

  return {
    event_id: uuid(),
    event_type: eventType,
    type_id: trap.type_id,
    type_version: trap.type_version,
    profile_revision: 1,
    occurred_at: occurredAt,
    session_id: uuid(),
    session_sequence: 1,
    source: { ip: atk.ip, port: 40000 + Math.floor(Math.random() * 20000) },
    destination: { protocol: proto, port: destPort },
    trap_id: trap.id,
    received_at: occurredAt,
    data,
    source_enrichment: { country_code: atk.cc, asn: atk.asn },
  }
}

// Засев истории за последние сутки
;(function seed() {
  for (let i = 0; i < 180; i++) {
    const trap = pickTrap()
    events.push(genEvent(trap, minsAgo(Math.floor(Math.random() * 60 * 24))))
  }
  events.sort((a, b) => b.occurred_at.localeCompare(a.occurred_at))
})()

// --- Команды ---------------------------------------------------------------

const commandsByTrap = new Map<string, Command[]>()

// --- Live-стрим (замена WSS /api/stream) -----------------------------------

type Listener = (n: StreamNotification) => void
const listeners = new Set<Listener>()
let timer: ReturnType<typeof setInterval> | null = null

function emit(n: StreamNotification) {
  listeners.forEach((l) => l(n))
}

function startLive() {
  if (timer) return
  timer = setInterval(() => {
    const trap = pickTrap()
    if (!trap) return
    const ev = genEvent(trap, now())
    events.unshift(ev)
    if (events.length > 2000) events.pop()
    const { data, source_enrichment, ...summary } = ev
    void data
    void source_enrichment
    emit({ kind: 'event.created', event: summary as EventSummary })
  }, 3500)
}

export const mockApi = {
  getSession(): SessionView {
    return {
      user: USER,
      organization: ORG,
      expires_at: new Date(Date.now() + 24 * 3600 * 1000).toISOString(),
      csrf_token: 'mock-csrf-' + uuid().slice(0, 8),
    }
  },

  listCatalog() {
    return { items: CATALOG, next_cursor: null }
  },

  listProfiles() {
    return { items: profiles, next_cursor: null }
  },
  getProfile(id: string) {
    return profiles.find((p) => p.id === id)
  },
  createProfile(input: {
    name: string
    description?: string
    type_id: string
    type_version: number
    config: Record<string, unknown>
  }): Profile {
    // writeOnly-поля (password) не возвращаются — фиксируем их как secret_fields_set
    const config = structuredClone(input.config)
    const secretFields = input.type_id === 'redis-emulator' ? ['/services/0/password'] : []
    if (input.type_id === 'honeytoken-http') {
      const tokens = config.tokens as { value?: string }[]
      tokens.forEach((token, i) => {
        secretFields.push(`/tokens/${i}/value`)
        delete token.value
      })
    }
    const level = input.type_id === 'redis-emulator' || input.type_id === 'honeytoken-http' ? 'medium' : 'low'
    const profile: Profile = {
      id: uuid(),
      name: input.name,
      description: input.description ?? '',
      type_id: input.type_id,
      type_version: input.type_version,
      interaction_level: level,
      config,
      secret_fields_set: secretFields,
      revision: 1,
      created_at: now(),
      updated_at: now(),
    }
    profiles.unshift(profile)
    return profile
  },

  listTraps(filter?: { connectivity?: string; profile_id?: string }) {
    let items = traps.slice()
    if (filter?.connectivity) items = items.filter((t) => t.connectivity === filter.connectivity)
    if (filter?.profile_id) items = items.filter((t) => t.profile_id === filter.profile_id)
    return { items, next_cursor: null }
  },
  getTrap(id: string) {
    return traps.find((t) => t.id === id)
  },
  createTrap(input: { name: string; description?: string; profile_id: string }): Trap {
    const profile = profiles.find((p) => p.id === input.profile_id)!
    const trap = mkTrap(input.name, profile, 'offline', 'unknown', 'pending')
    trap.description = input.description ?? ''
    trap.runtime_state = 'unknown'
    trap.desired_state = 'stopped'
    trap.applied_profile_revision = null
    trap.agent = null
    traps.unshift(trap)
    emit({ kind: 'trap.changed', trap })
    return trap
  },
  deleteTrap(id: string) {
    const idx = traps.findIndex((t) => t.id === id)
    if (idx >= 0) {
      traps.splice(idx, 1)
      emit({ kind: 'trap.deleted', trap_id: id })
    }
  },

  createCommand(trapId: string, action: CommandAction): Command {
    const trap = traps.find((t) => t.id === trapId)!
    const cmd: Command = {
      id: uuid(),
      trap_id: trapId,
      request_id: uuid(),
      action,
      params: {},
      status: 'queued',
      target_profile_revision: action === 'stop' ? null : trap.desired_profile_revision,
      created_at: now(),
      expires_at: new Date(Date.now() + 60_000).toISOString(),
      started_at: null,
      finished_at: null,
      result: null,
      error: null,
    }
    const list = commandsByTrap.get(trapId) ?? []
    list.unshift(cmd)
    commandsByTrap.set(trapId, list)
    trap.active_command_id = cmd.id
    emit({ kind: 'command.changed', trap_id: trapId, command: cmd })

    // Симуляция выполнения
    setTimeout(() => {
      cmd.status = 'running'
      cmd.started_at = now()
      emit({ kind: 'command.changed', trap_id: trapId, command: { ...cmd } })
    }, 700)
    setTimeout(() => {
      cmd.status = 'succeeded'
      cmd.finished_at = now()
      cmd.result = { runtime_state: action === 'stop' ? 'stopped' : 'running' }
      trap.runtime_state = action === 'stop' ? 'stopped' : 'running'
      trap.desired_state = action === 'stop' ? 'stopped' : 'running'
      if (action !== 'stop') trap.applied_profile_revision = trap.desired_profile_revision
      trap.active_command_id = null
      trap.state_version += 1
      trap.updated_at = now()
      emit({ kind: 'command.changed', trap_id: trapId, command: { ...cmd } })
      emit({ kind: 'trap.changed', trap: { ...trap } })
    }, 2000)
    return cmd
  },
  listCommands(trapId: string) {
    return { items: commandsByTrap.get(trapId) ?? [], next_cursor: null }
  },

  listEvents(filter?: { trap_id?: string; limit?: number }) {
    let items = events as EventSummary[]
    if (filter?.trap_id) items = items.filter((e) => e.trap_id === filter.trap_id)
    const limit = filter?.limit ?? 50
    return {
      items: items.slice(0, limit).map(({ ...e }) => e),
      next_cursor: null,
      stream_cursor: 'cursor-' + Date.now(),
    }
  },
  getEvent(id: string) {
    return events.find((e) => e.event_id === id)
  },
  allEvents() {
    return events
  },

  subscribe(listener: Listener): () => void {
    listeners.add(listener)
    startLive()
    return () => listeners.delete(listener)
  },
}
