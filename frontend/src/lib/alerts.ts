import type { CommandAction, StreamNotification, Trap } from '../types'

export interface AlertInput {
  id: string
  title: string
  message?: string
  level: 'info' | 'success' | 'warning' | 'error'
  href?: string
}
export interface AlertItem extends AlertInput {
  createdAt: number
  read: boolean
  toast: boolean
}
type AlertAction =
  | { type: 'add'; alert: AlertInput; now: number }
  | { type: 'dismiss' | 'read'; id: string }
  | { type: 'read-all' | 'clear' }

export function alertReducer(state: AlertItem[], action: AlertAction): AlertItem[] {
  switch (action.type) {
    case 'add': {
      if (state.some((a) => a.id === action.alert.id)) return state
      let visible = 1
      return [{ ...action.alert, createdAt: action.now, read: false, toast: true },
        ...state.map((a) => a.toast && ++visible > 3 ? { ...a, toast: false } : a)].slice(0, 100)
    }
    case 'dismiss': return state.map((a) => a.id === action.id ? { ...a, toast: false } : a)
    case 'read': return state.map((a) => a.id === action.id ? { ...a, read: true, toast: false } : a)
    case 'read-all': return state.map((a) => ({ ...a, read: true }))
    case 'clear': return []
  }
}

export function actionErrorMessage(error: unknown): string {
  const status = error && typeof error === 'object' && 'status' in error ? error.status : undefined
  switch (status) {
    case 401: return 'Ваша сессия истекла. Войдите снова.'
    case 403: return 'Недостаточно прав для этого действия.'
    case 404: return 'Объект уже недоступен. Обновите страницу.'
    case 409: return 'Данные изменились или действие уже выполняется. Обновите страницу и повторите.'
    case 400:
    case 422: return 'Проверьте введённые данные.'
    default: return typeof status === 'number'
      ? 'Не удалось выполнить действие. Попробуйте ещё раз.'
      : 'Проверьте соединение и попробуйте ещё раз.'
  }
}

const commandNames: Record<CommandAction, string> = { start: 'Запуск', stop: 'Остановка', apply_config: 'Применение конфигурации' }
type Snapshot = { version: number; name: string; connectivity: string; runtime: string; buffer?: string }
const snapshot = (trap: Trap): Snapshot => ({ version: trap.state_version, name: trap.name,
  connectivity: trap.connectivity, runtime: trap.runtime_state, buffer: trap.agent?.buffer_state })

// This tracker belongs to one authenticated layout. Never persist payloads or reuse across accounts.
export function createAlertTracker() {
  const traps = new Map<string, Snapshot>()
  const seen = new Set<string>()
  const remember = (key: string) => {
    if (seen.has(key)) return false
    seen.add(key)
    if (seen.size > 1000) seen.delete(seen.values().next().value!)
    return true
  }
  const store = (id: string, next: Snapshot) => {
    traps.set(id, next)
    if (traps.size > 1000) traps.delete(traps.keys().next().value!)
  }
  return {
    seed(items: Trap[]) {
      for (const trap of items) {
        if (seen.has(`deleted:${trap.id}`)) continue
        const old = traps.get(trap.id)
        // REST advances the baseline silently so older replay cannot produce false alerts.
        if (!old || old.version < trap.state_version) store(trap.id, snapshot(trap))
      }
    },
    accept(notification: StreamNotification): AlertInput[] {
      switch (notification.kind) {
        case 'profile.deleted': return []
        case 'event.created': {
          const e = notification.event
          const id = `event:${e.event_id}`
          if (!remember(id)) return []
          return [{ id, title: 'Новое событие ловушки', level: 'warning',
            message: `${e.event_type} · ${e.source.ip}:${e.source.port} → ${e.destination.protocol}:${e.destination.port}`,
            href: `/app/events?event=${encodeURIComponent(e.event_id)}` }]
        }
        case 'trap.changed': {
          const t = notification.trap
          const old = traps.get(t.id)
          if (seen.has(`deleted:${t.id}`) || (old && old.version >= t.state_version)) return []
          const next = snapshot(t)
          store(t.id, next)
          if (!old) return []
          const alerts: AlertInput[] = []
          const add = (field: string, title: string, level: AlertInput['level']) => alerts.push({
            id: `trap:${t.id}:${t.state_version}:${field}`, title, level, message: t.name,
            href: `/app/traps/${encodeURIComponent(t.id)}`,
          })
          if (old.connectivity !== next.connectivity)
            add('connectivity', next.connectivity === 'online' ? 'Ловушка снова на связи' : 'Ловушка потеряла связь', next.connectivity === 'online' ? 'success' : 'warning')
          if (old.runtime !== next.runtime) {
            const labels = { running: 'Ловушка запущена', stopped: 'Ловушка остановлена', error: 'Ошибка ловушки', unknown: 'Состояние ловушки неизвестно' }
            add('runtime', labels[t.runtime_state], t.runtime_state === 'error' ? 'error' : t.runtime_state === 'running' ? 'success' : 'warning')
          }
          if (old.buffer !== next.buffer && next.buffer) {
            const labels = { ok: 'Буфер событий восстановлен', full: 'Буфер событий заполнен', unavailable: 'Буфер событий недоступен' }
            if (next.buffer !== 'ok' || old.buffer) add('buffer', labels[t.agent!.buffer_state], next.buffer === 'ok' ? 'success' : 'warning')
          }
          return alerts
        }
        case 'trap.deleted': {
          const id = `deleted:${notification.trap_id}`
          if (!remember(id)) return []
          const name = traps.get(notification.trap_id)?.name
          traps.delete(notification.trap_id)
          return [{ id, title: 'Ловушка удалена', message: name, level: 'info', href: '/app/traps' }]
        }
        case 'command.changed': {
          const c = notification.command
          if (!['succeeded', 'failed', 'expired'].includes(c.status)) return []
          const id = `command:${c.id}`
          if (!remember(id)) return []
          const result = c.status === 'succeeded' ? 'выполнено' : c.status === 'expired' ? 'время ожидания истекло' : 'ошибка выполнения'
          return [{ id, title: `${commandNames[c.action]}: ${result}`, message: traps.get(c.trap_id)?.name,
            level: c.status === 'succeeded' ? 'success' : 'error', href: `/app/traps/${encodeURIComponent(c.trap_id)}` }]
        }
      }
    },
  }
}
