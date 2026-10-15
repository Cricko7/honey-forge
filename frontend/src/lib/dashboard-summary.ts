import type { EventSummary } from '../types'

const hour = 60 * 60 * 1000

export function summarizeDashboard(events: EventSummary[], now = Date.now()) {
  const cutoff = now - 24 * hour
  const start = Math.floor(cutoff / hour) * hour
  const activity = Array.from({ length: Math.floor((now - start) / hour) + 1 }, (_, index) => ({
    at: start + index * hour,
    count: 0,
  }))
  const types = new Map<string, number>()
  const sources = new Map<string, number>()
  let events24h = 0
  let authAttempts = 0

  for (const event of events) {
    const at = Date.parse(event.occurred_at)
    if (at >= cutoff && at <= now) {
      activity[Math.floor((at - start) / hour)].count++
      events24h++
    }
    if (event.event_type.includes('auth')) authAttempts++
    types.set(event.event_type, (types.get(event.event_type) ?? 0) + 1)
    sources.set(event.source.ip, (sources.get(event.source.ip) ?? 0) + 1)
  }

  return {
    activity,
    events24h,
    authAttempts,
    uniqueSources: sources.size,
    byType: [...types].map(([type, count]) => ({ type, count })).sort((a, b) => b.count - a.count),
    sources: [...sources].map(([ip, count]) => ({ ip, count })).sort((a, b) => b.count - a.count).slice(0, 6),
  }
}
