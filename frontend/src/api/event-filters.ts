import type { EventSummary } from '@/types'

export interface EventFilter {
  trap_id?: string
  type_id?: string
  event_type?: string
  from?: string
  to?: string
  limit?: number
  cursor?: string
}

export function eventQuery(filter: EventFilter = {}, exportFormat?: 'csv' | 'stix'): string {
  const query = new URLSearchParams()
  for (const key of ['trap_id', 'type_id', 'event_type', 'from', 'to'] as const) {
    if (filter[key]) query.set(key, filter[key])
  }
  if (exportFormat) query.set('format', exportFormat)
  else {
    query.set('limit', String(filter.limit ?? 50))
    if (filter.cursor) query.set('cursor', filter.cursor)
  }
  return query.toString()
}

export function matchesEvent(event: EventSummary, filter: EventFilter = {}): boolean {
  const at = new Date(event.occurred_at).getTime()
  return (!filter.trap_id || event.trap_id === filter.trap_id)
    && (!filter.type_id || event.type_id === filter.type_id)
    && (!filter.event_type || event.event_type === filter.event_type)
    && (!filter.from || at >= new Date(filter.from).getTime())
    && (!filter.to || at < new Date(filter.to).getTime())
}

export function journalFilter(values: { trap_id: string; type_id: string; event_type: string; from: string; to: string }): EventFilter {
  const filter: EventFilter = {}
  for (const key of ['trap_id', 'type_id', 'event_type'] as const) if (values[key]) filter[key] = values[key]
  for (const key of ['from', 'to'] as const) {
    if (values[key]) {
      const date = new Date(values[key])
      if (Number.isNaN(date.getTime())) throw new Error('Укажите корректные даты')
      filter[key] = date.toISOString()
    }
  }
  if (filter.from && filter.to && filter.from >= filter.to) throw new Error('Дата «До» должна быть позже даты «С»')
  return filter
}
