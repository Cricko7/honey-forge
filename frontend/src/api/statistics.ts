import type { EventPage, EventSummary } from '../types'
import type { EventFilter } from './event-filters'

export const MAX_SAMPLE_SIZE = 1000

export function parseSampleSize(value: string): number | null {
  if (!/^[1-9]\d{0,3}$/.test(value)) return null
  const size = Number(value)
  return size <= MAX_SAMPLE_SIZE ? size : null
}

export interface StatisticsSample {
  items: EventSummary[]
  hasMore: boolean
}

// Use the existing organization-scoped summary endpoint and its immutable cursor
// boundary. A failed page must never appear as a complete, smaller sample.
export async function loadStatisticsSample(
  listEvents: (filter: EventFilter, signal?: AbortSignal) => Promise<EventPage>,
  size: number,
  signal?: AbortSignal,
): Promise<StatisticsSample> {
  if (!Number.isInteger(size) || size < 1 || size > MAX_SAMPLE_SIZE) throw new Error('Invalid sample size')
  const items: EventSummary[] = []
  const ids = new Set<string>()
  const cursors = new Set<string>()
  let cursor: string | undefined
  while (items.length < size) {
    signal?.throwIfAborted()
    const limit = Math.min(100, size - items.length)
    const page = await listEvents({ limit, ...(cursor ? { cursor } : {}) }, signal)
    signal?.throwIfAborted()
    if (page.items.length > limit || (page.next_cursor && page.items.length !== limit)) throw new Error('Invalid event page')
    for (const event of page.items) {
      if (ids.has(event.event_id)) throw new Error('Duplicate event in snapshot')
      ids.add(event.event_id)
      items.push(event)
    }
    if (!page.next_cursor) return { items, hasMore: false }
    if (cursors.has(page.next_cursor)) throw new Error('Repeated event cursor')
    cursors.add(page.next_cursor)
    cursor = page.next_cursor
  }
  return { items, hasMore: true }
}
