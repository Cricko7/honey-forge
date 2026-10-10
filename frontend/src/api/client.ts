import type {
  CatalogEntry,
  Command,
  CommandAction,
  Event,
  EventPage,
  Page,
  Profile,
  SessionView,
  StreamNotification,
  Trap,
} from '@/types'
import { mockApi } from './mock'

// Переключатель источника данных. Пусто → mock-слой (демо хакатона).
// Для реального backend: VITE_API_BASE=https://api.example.org
const API_BASE = import.meta.env.VITE_API_BASE ?? ''
export const USE_MOCK = API_BASE === ''

// Имитация сетевой задержки для mock, чтобы спиннеры/skeleton были видны.
const delay = (ms = 220) => new Promise((r) => setTimeout(r, ms))

let csrfToken = ''

async function real<T>(path: string, init?: RequestInit): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(init?.headers as Record<string, string>),
  }
  const method = init?.method ?? 'GET'
  if (method !== 'GET' && csrfToken) {
    headers['X-CSRF-Token'] = csrfToken
    headers['Origin'] = window.location.origin
  }
  const res = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers,
    credentials: 'include',
  })
  if (!res.ok) {
    const body = await res.json().catch(() => null)
    throw new ApiError(res.status, body?.error?.code ?? 'unknown', body?.error?.message ?? res.statusText)
  }
  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message)
  }
}

export const api = {
  async getSession(): Promise<SessionView> {
    if (USE_MOCK) {
      await delay()
      const s = mockApi.getSession()
      csrfToken = s.csrf_token
      return s
    }
    const s = await real<SessionView>('/api/session')
    csrfToken = s.csrf_token
    return s
  },

  async login(email: string, password: string): Promise<SessionView> {
    if (USE_MOCK) {
      await delay(400)
      void email
      void password
      const s = mockApi.getSession()
      csrfToken = s.csrf_token
      return s
    }
    const s = await real<SessionView>('/api/sessions', {
      method: 'POST',
      body: JSON.stringify({ email, password }),
    })
    csrfToken = s.csrf_token
    return s
  },

  // Регистрация первого админа + организации (mode:create) — настоящий вход
  // на свежем бэке. Атомарно создаёт org + admin + сессию, возвращает SessionView.
  async register(email: string, password: string, organizationName: string): Promise<SessionView> {
    if (USE_MOCK) {
      await delay(400)
      void organizationName
      const s = mockApi.getSession()
      s.user.email = email
      csrfToken = s.csrf_token
      return s
    }
    const s = await real<SessionView>('/api/registrations', {
      method: 'POST',
      body: JSON.stringify({
        email,
        password,
        organization: { mode: 'create', name: organizationName },
      }),
    })
    csrfToken = s.csrf_token
    return s
  },

  async logout(): Promise<void> {
    if (USE_MOCK) return
    await real<void>('/api/session', { method: 'DELETE' })
  },

  // Создание профиля под выбранный тип каталога. config — валидный JSON под
  // config_schema типа; секреты (writeOnly) передаются в config при создании.
  async createProfile(input: {
    name: string
    description?: string
    type_id: string
    type_version: number
    config: Record<string, unknown>
  }): Promise<Profile> {
    if (USE_MOCK) {
      await delay(300)
      return mockApi.createProfile(input)
    }
    return real('/api/profiles', {
      method: 'POST',
      body: JSON.stringify({ request_id: crypto.randomUUID(), ...input }),
    })
  },

  async listCatalog(): Promise<Page<CatalogEntry>> {
    if (USE_MOCK) {
      await delay()
      return mockApi.listCatalog()
    }
    return real('/api/trap-types?limit=100')
  },

  async listProfiles(): Promise<Page<Profile>> {
    if (USE_MOCK) {
      await delay()
      return mockApi.listProfiles()
    }
    return real('/api/profiles?limit=100')
  },

  async listTraps(filter?: { connectivity?: string; profile_id?: string }): Promise<Page<Trap>> {
    if (USE_MOCK) {
      await delay()
      return mockApi.listTraps(filter)
    }
    const qs = new URLSearchParams({ limit: '100' })
    if (filter?.connectivity) qs.set('connectivity', filter.connectivity)
    if (filter?.profile_id) qs.set('profile_id', filter.profile_id)
    return real(`/api/traps?${qs}`)
  },

  async getTrap(id: string): Promise<Trap> {
    if (USE_MOCK) {
      await delay()
      const t = mockApi.getTrap(id)
      if (!t) throw new ApiError(404, 'resource_not_found', 'Resource not found')
      return t
    }
    return real(`/api/traps/${id}`)
  },

  async createTrap(input: { name: string; description?: string; profile_id: string }): Promise<Trap> {
    if (USE_MOCK) {
      await delay(300)
      return mockApi.createTrap(input)
    }
    return real('/api/traps', {
      method: 'POST',
      body: JSON.stringify({ request_id: crypto.randomUUID(), ...input }),
    })
  },

  async deleteTrap(id: string, expectedRevision: number): Promise<void> {
    if (USE_MOCK) {
      await delay(200)
      mockApi.deleteTrap(id)
      return
    }
    await real(`/api/traps/${id}`, {
      method: 'DELETE',
      headers: { 'X-Expected-Revision': String(expectedRevision) },
    })
  },

  async createCommand(trapId: string, action: CommandAction): Promise<Command> {
    if (USE_MOCK) {
      await delay(200)
      return mockApi.createCommand(trapId, action)
    }
    return real(`/api/traps/${trapId}/commands`, {
      method: 'POST',
      body: JSON.stringify({ request_id: crypto.randomUUID(), action, params: {} }),
    })
  },

  async listCommands(trapId: string): Promise<Page<Command>> {
    if (USE_MOCK) {
      await delay()
      return mockApi.listCommands(trapId)
    }
    return real(`/api/traps/${trapId}/commands?limit=50`)
  },

  async listEvents(filter?: { trap_id?: string; limit?: number }): Promise<EventPage> {
    if (USE_MOCK) {
      await delay()
      return mockApi.listEvents(filter)
    }
    const qs = new URLSearchParams({ limit: String(filter?.limit ?? 50) })
    if (filter?.trap_id) qs.set('trap_id', filter.trap_id)
    return real(`/api/events?${qs}`)
  },

  async getEvent(id: string): Promise<Event> {
    if (USE_MOCK) {
      await delay()
      const e = mockApi.getEvent(id)
      if (!e) throw new ApiError(404, 'resource_not_found', 'Resource not found')
      return e
    }
    return real(`/api/events/${id}`)
  },
}

// --- Поток уведомлений дашборда (WSS /api/stream или mock pub/sub) ---------

export function subscribeStream(onNotify: (n: StreamNotification) => void): () => void {
  if (USE_MOCK) {
    return mockApi.subscribe(onNotify)
  }
  // Реальный WSS: dashboard-stream.v1, replay→ready→live.
  const wsBase = API_BASE.replace(/^http/, 'ws')
  const ws = new WebSocket(`${wsBase}/api/stream`)
  ws.onopen = () => {
    ws.send(JSON.stringify({ message_id: crypto.randomUUID(), type: 'stream.subscribe', payload: { after: null } }))
  }
  ws.onmessage = (ev) => {
    try {
      const env = JSON.parse(ev.data)
      const data = env.payload?.data ?? env.payload
      if (env.type === 'event.created' && data?.event) onNotify({ kind: 'event.created', event: data.event })
      else if (env.type === 'trap.changed' && data?.trap) onNotify({ kind: 'trap.changed', trap: data.trap })
      else if (env.type === 'trap.deleted' && data?.trap_id)
        onNotify({ kind: 'trap.deleted', trap_id: data.trap_id })
      else if (env.type === 'command.changed' && data?.command)
        onNotify({ kind: 'command.changed', trap_id: data.trap_id, command: data.command })
    } catch {
      /* ignore */
    }
  }
  return () => ws.close()
}
