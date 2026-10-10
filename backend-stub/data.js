// Демо-данные и генератор атак для стаб-бэкенда HoneyForge.
// Форма DTO строго по api/openapi.yaml. Логика перенесена из фронтового mock.
import { randomUUID } from 'node:crypto'

const now = () => new Date().toISOString()
const minsAgo = (m) => new Date(Date.now() - m * 60_000).toISOString()
const pick = (arr) => arr[Math.floor(Math.random() * arr.length)]

export const uuid = () => randomUUID()

// --- Каталог типов (immutable) ---------------------------------------------

export const CATALOG = [
  {
    type_id: 'tcp-banner',
    type_version: 1,
    title: 'TCP Banner',
    description:
      'Низкоинтерактивная ловушка: открывает TCP-порты, отдаёт баннер и логирует подключения и payload атакующего.',
    interaction_level: 'low',
    available_for_new_profiles: true,
    config_schema: { type: 'object', required: ['listeners', 'logging', 'management'], properties: {} },
    event_schemas: [
      { event_type: 'tcp.connection_opened', title: 'Подключение открыто', data_schema: {} },
      { event_type: 'tcp.payload_received', title: 'Получен payload', data_schema: {} },
      { event_type: 'tcp.connection_closed', title: 'Подключение закрыто', data_schema: {} },
    ],
    actions: [
      { action: 'start', title: 'Запустить', params_schema: {}, result_schema: {} },
      { action: 'stop', title: 'Остановить', params_schema: {}, result_schema: {} },
      { action: 'apply_config', title: 'Применить конфиг', params_schema: {}, result_schema: {} },
    ],
    ui: { field_order: ['/listeners', '/logging', '/management'], widgets: {} },
  },
  {
    type_id: 'redis-emulator',
    type_version: 1,
    title: 'Redis Emulator',
    description:
      'Среднеинтерактивная ловушка: эмулирует Redis, ловит попытки аутентификации и команды атакующего.',
    interaction_level: 'medium',
    available_for_new_profiles: true,
    config_schema: { type: 'object', required: ['services', 'management'], properties: {} },
    event_schemas: [
      { event_type: 'service.connection_opened', title: 'Подключение открыто', data_schema: {} },
      { event_type: 'service.auth_attempt', title: 'Попытка входа', data_schema: {} },
      { event_type: 'service.action', title: 'Команда сервиса', data_schema: {} },
      { event_type: 'service.connection_closed', title: 'Подключение закрыто', data_schema: {} },
    ],
    actions: [
      { action: 'start', title: 'Запустить', params_schema: {}, result_schema: {} },
      { action: 'stop', title: 'Остановить', params_schema: {}, result_schema: {} },
      { action: 'apply_config', title: 'Применить конфиг', params_schema: {}, result_schema: {} },
    ],
    ui: { field_order: ['/services', '/management'], widgets: { '/services/0/password': 'password' } },
  },
]

// --- Организация и пользователь ---------------------------------------------

export const ORG = { id: uuid(), name: 'ООО «Периметр»', created_at: minsAgo(60 * 24 * 90) }
export const USER = { id: uuid(), email: 'admin@perimetr.ru', role: 'admin' }

export function sessionView() {
  return {
    user: USER,
    organization: ORG,
    expires_at: new Date(Date.now() + 24 * 3600 * 1000).toISOString(),
    csrf_token: 'stub-csrf-' + uuid().slice(0, 8),
  }
}

// --- Профили ----------------------------------------------------------------

function mkProfile(name, typeId, level, config, secretFields = []) {
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

export const profiles = [
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

export function createProfile({ name, description, type_id, type_version, config }) {
  const secretFields = type_id === 'redis-emulator' ? ['/services/0/password'] : []
  const level = type_id === 'redis-emulator' ? 'medium' : 'low'
  const profile = {
    id: uuid(),
    name,
    description: description ?? '',
    type_id,
    type_version,
    interaction_level: level,
    config,
    secret_fields_set: secretFields,
    revision: 1,
    created_at: now(),
    updated_at: now(),
  }
  profiles.unshift(profile)
  return profile
}

// --- Ловушки ----------------------------------------------------------------

function mkTrap(name, profile, conn, runtime, hostname) {
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

export const traps = [
  mkTrap('dmz-ssh-01', profiles[0], 'online', 'running', 'dmz-host-01'),
  mkTrap('dmz-ftp-01', profiles[1], 'online', 'running', 'dmz-host-02'),
  mkTrap('lan-redis-01', profiles[2], 'online', 'running', 'lan-host-07'),
  mkTrap('lan-ssh-02', profiles[0], 'offline', 'stopped', 'lan-host-09'),
  mkTrap('edge-ftp-02', profiles[1], 'online', 'error', 'edge-host-03'),
]

export function createTrap({ name, description, profile_id }) {
  const profile = profiles.find((p) => p.id === profile_id)
  if (!profile) return null
  const trap = mkTrap(name, profile, 'offline', 'unknown', 'pending')
  trap.description = description ?? ''
  trap.desired_state = 'stopped'
  trap.applied_profile_revision = null
  trap.agent = null
  traps.unshift(trap)
  return trap
}

// --- События ----------------------------------------------------------------

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

export const events = []

function onlineTraps() {
  return traps.filter((t) => t.connectivity === 'online')
}

export function genEvent(trap, occurredAt) {
  const atk = pick(ATTACKER_IPS)
  const isRedis = trap.type_id === 'redis-emulator'
  let eventType
  let data
  let destPort
  let proto

  if (isRedis) {
    destPort = 6379
    proto = 'redis'
    const roll = Math.random()
    if (roll < 0.45) {
      const c = pick(CREDS)
      eventType = 'service.auth_attempt'
      data = { username: c.u, password: c.p, outcome: 'rejected', received_bytes: 48 }
    } else if (roll < 0.85) {
      eventType = 'service.action'
      data = { input: pick(REDIS_CMDS), received_bytes: 32 }
    } else {
      eventType = 'service.connection_opened'
      data = {}
    }
  } else {
    proto = trap.name.includes('ftp') ? 'ftp' : 'ssh'
    destPort = proto === 'ftp' ? 21 : 22
    const roll = Math.random()
    if (roll < 0.5) {
      const c = pick(CREDS)
      const payload = `${c.u}:${c.p}`
      eventType = 'tcp.payload_received'
      data = {
        listener_name: proto,
        payload_base64: Buffer.from(payload, 'utf8').toString('base64'),
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

export function summary(ev) {
  const { data, source_enrichment, ...rest } = ev
  void data
  void source_enrichment
  return rest
}

// Засев истории
;(function seed() {
  for (let i = 0; i < 180; i++) {
    events.push(genEvent(pick(onlineTraps()), minsAgo(Math.floor(Math.random() * 60 * 24))))
  }
  events.sort((a, b) => b.occurred_at.localeCompare(a.occurred_at))
})()

export function nextLiveEvent() {
  const on = onlineTraps()
  if (!on.length) return null
  const ev = genEvent(pick(on), now())
  events.unshift(ev)
  if (events.length > 2000) events.pop()
  return ev
}

// --- Команды ----------------------------------------------------------------

export const commandsByTrap = new Map()

export function createCommand(trap, action, onChange) {
  const cmd = {
    id: uuid(),
    trap_id: trap.id,
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
  const list = commandsByTrap.get(trap.id) ?? []
  list.unshift(cmd)
  commandsByTrap.set(trap.id, list)
  trap.active_command_id = cmd.id
  onChange?.({ kind: 'command.changed', trap_id: trap.id, command: { ...cmd } })

  setTimeout(() => {
    cmd.status = 'running'
    cmd.started_at = now()
    onChange?.({ kind: 'command.changed', trap_id: trap.id, command: { ...cmd } })
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
    onChange?.({ kind: 'command.changed', trap_id: trap.id, command: { ...cmd } })
    onChange?.({ kind: 'trap.changed', trap: { ...trap } })
  }, 2000)

  return cmd
}
