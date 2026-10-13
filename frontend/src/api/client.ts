import type {
  DeploymentNode,
  SessionRecording,
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
  TrapLink,
} from '@/types'
import { registrationOrganization, type RegistrationMode } from '@/lib/auth'
import { eventQuery, type EventFilter } from './event-filters'
import { mockApi } from './mock'
import { subscribeDashboardStream } from './stream'
import { ApiError, createHTTPClient } from './http'

export { ApiError } from './http'

export interface APIKey {
  id: string
  name: string
  prefix: string
  created_at: string
}

// Переключатель источника данных. Пусто → mock-слой (демо хакатона).
// Для реального backend: VITE_API_BASE=https://api.example.org
const API_BASE = import.meta.env.VITE_API_BASE ?? ''
export const USE_MOCK = API_BASE === ''

// Имитация сетевой задержки для mock, чтобы спиннеры/skeleton были видны.
const delay = (ms = 220) => new Promise((r) => setTimeout(r, ms))

let csrfToken = ''
let mockSignedOut = false

const real = createHTTPClient(API_BASE, () => csrfToken)

export const api = {
  async listAPIKeys(): Promise<{ items: APIKey[] }> {
    if (USE_MOCK) throw new ApiError(503, 'demo_unavailable', 'API-ключи доступны с подключённым сервером')
    return real('/api/api-keys')
  },

  async createAPIKey(name: string): Promise<APIKey & { token: string }> {
    if (USE_MOCK) throw new ApiError(503, 'demo_unavailable', 'API-ключи доступны с подключённым сервером')
    return real('/api/api-keys', { method: 'POST', body: JSON.stringify({ name }) })
  },

  async revokeAPIKey(id: string): Promise<void> {
    if (USE_MOCK) throw new ApiError(503, 'demo_unavailable', 'API-ключи доступны с подключённым сервером')
    await real(`/api/api-keys/${encodeURIComponent(id)}`, { method: 'DELETE' })
  },

  async listTrapLinks(): Promise<{ items: TrapLink[] }> {
    if (USE_MOCK) { await delay(); return mockApi.listTrapLinks() }
    return real('/api/trap-links')
  },
  async createTrapLink(link: TrapLink): Promise<TrapLink> {
    if (USE_MOCK) {
      await delay()
      if (link.source_id === link.target_id) throw new ApiError(422, 'validation_failed', 'Нельзя связать ловушку с собой')
      if (!mockApi.getTrap(link.source_id) || !mockApi.getTrap(link.target_id)) throw new ApiError(404, 'resource_not_found', 'Ловушка удалена')
      if (mockApi.listTrapLinks().items.some((l) => l.source_id === link.source_id && l.target_id === link.target_id)) throw new ApiError(409, 'trap_link_exists', 'Связь уже существует')
      return mockApi.createTrapLink(link)
    }
    return real('/api/trap-links', { method: 'POST', body: JSON.stringify(link) })
  },
  async deleteTrapLink(link: TrapLink): Promise<void> {
    if (USE_MOCK) { await delay(); mockApi.deleteTrapLink(link); return }
    await real(`/api/trap-links/${link.source_id}/${link.target_id}`, { method: 'DELETE' })
  },
  async getSession(): Promise<SessionView> {
    if (USE_MOCK) {
      await delay()
      if (mockSignedOut) throw new ApiError(401, 'unauthenticated', 'Войдите в демо')
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
      mockSignedOut = false
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
  async register(email: string, password: string, organizationName: string, mode: RegistrationMode = 'create'): Promise<SessionView> {
    if (USE_MOCK) {
      if (mode === 'join') throw new ApiError(503, 'demo_unavailable', 'Регистрация по коду доступна с подключённым сервером')
      await delay(400)
      void organizationName
      mockSignedOut = false
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
        organization: registrationOrganization(mode, organizationName),
      }),
    })
    csrfToken = s.csrf_token
    return s
  },

  async getJoinCode(): Promise<{ code: string; revision: number; rotated_at: string }> {
    if (USE_MOCK) throw new ApiError(503, 'demo_unavailable', 'Коды приглашения доступны с подключённым сервером')
    return real('/api/organization/join-code')
  },

  async rotateJoinCode(expectedRevision: number): Promise<{ code: string; revision: number; rotated_at: string }> {
    if (USE_MOCK) throw new ApiError(503, 'demo_unavailable', 'Коды приглашения доступны с подключённым сервером')
    return real('/api/organization/join-code/rotations', {
      method: 'POST', body: JSON.stringify({ expected_revision: expectedRevision }),
    })
  },

  async logout(): Promise<void> {
    if (USE_MOCK) mockSignedOut = true
    else await real<void>('/api/session', { method: 'DELETE' })
    csrfToken = ''
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

  async listDeploymentNodes(): Promise<{ items: DeploymentNode[] }> {
    if (USE_MOCK) return { items: [] }
    return real("/api/traps/deployment-nodes")
  },

  async createTrap(input: { name: string; description?: string; profile_id: string; deployment_node_id?: string }): Promise<Trap> {
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

  // params валидируется под дескриптор действия типа ловушки: start/stop → {},
  // apply_config → {profile_revision}. Пустой объект для start/stop обязателен.
  async createCommand(
    trapId: string,
    action: CommandAction,
    params: Record<string, unknown> = {},
  ): Promise<Command> {
    if (USE_MOCK) {
      await delay(200)
      return mockApi.createCommand(trapId, action)
    }
    return real(`/api/traps/${trapId}/commands`, {
      method: 'POST',
      body: JSON.stringify({ request_id: crypto.randomUUID(), action, params }),
    })
  },

  async listCommands(trapId: string): Promise<Page<Command>> {
    if (USE_MOCK) {
      await delay()
      return mockApi.listCommands(trapId)
    }
    return real(`/api/traps/${trapId}/commands?limit=50`)
  },

  async listEvents(filter?: EventFilter): Promise<EventPage> {
    if (USE_MOCK) {
      await delay()
      return mockApi.listEvents(filter)
    }
    const qs = eventQuery(filter)
    return real(`/api/events?${qs}`)
  },

  async exportEvents(format: 'csv' | 'stix', filter?: EventFilter): Promise<Blob> {
    if (USE_MOCK) {
      const { mockEventExport } = await import('./mock-export')
      return mockEventExport(mockApi.allEvents(), format, filter)
    }
    return real(`/api/events/export?${eventQuery(filter, format)}`, undefined, 'blob')
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

  async getRecording(trapId: string, sessionId: string): Promise<SessionRecording> {
    if (USE_MOCK) {
      await delay()
      const items = mockApi.listEvents({ trap_id: trapId, limit: 100 }).items
        .filter((e) => e.session_id === sessionId && e.type_id === 'ssh-high-interaction')
      const events = items.map((e) => mockApi.getEvent(e.event_id)!).sort((a, b) => a.session_sequence - b.session_sequence)
      return { trap_id: trapId, session_id: sessionId, complete: false, events }
    }
    return real(`/api/events/recordings/${encodeURIComponent(sessionId)}?trap_id=${encodeURIComponent(trapId)}`)
  },
}

// --- Поток уведомлений дашборда (WSS /api/stream или mock pub/sub) ---------

export function subscribeStream(onNotify: (n: StreamNotification) => void, options?: {
  onSynchronize?: () => Promise<unknown>
  onUnauthorized?: () => void
}): () => void {
  if (USE_MOCK) {
    return mockApi.subscribe(onNotify)
  }
  return subscribeDashboardStream(API_BASE, onNotify, {
    checkSession: api.getSession,
    synchronize: async () => {
      const page = await api.listEvents({ limit: 1 })
      await options?.onSynchronize?.()
      return page.stream_cursor
    },
    onUnauthorized: () => options?.onUnauthorized?.(),
  })
}
