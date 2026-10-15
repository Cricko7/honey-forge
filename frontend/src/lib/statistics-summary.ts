import type { EventSummary } from '../types'
import { eventSeverity } from './event-severity.ts'

const names = new Intl.DisplayNames(['ru'], { type: 'region', fallback: 'none' })

export function countryName(code: string | null | undefined): string | null {
  if (!code || !/^[A-Z]{2}$/.test(code) || ['ZZ', 'EU', 'UN'].includes(code)) return null
  return names.of(code) ?? null
}

export interface StatisticCount { key: string; count: number }
export interface CountryCount extends StatisticCount { sources: number }

function increment(counts: Map<string, number>, key: string) {
  counts.set(key, (counts.get(key) ?? 0) + 1)
}

function rank(counts: Map<string, number>): StatisticCount[] {
  return [...counts].map(([key, count]) => ({ key, count }))
    .sort((a, b) => b.count - a.count || a.key.localeCompare(b.key, 'en'))
}

export function summarizeStatistics(events: EventSummary[]) {
  const types = new Map<string, number>()
  const sources = new Map<string, number>()
  const traps = new Map<string, number>()
  const services = new Map<string, number>()
  const severities = new Map<string, number>()
  const countries = new Map<string, number>()
  const countrySources = new Map<string, Set<string>>()
  const asns = new Map<string, number>()
  const sessions = new Set<string>()
  const timestamps: number[] = []
  let authAttempts = 0
  let geoKnown = 0
  let asnKnown = 0
  let highSeverity = 0

  for (const event of events) {
    increment(types, event.event_type)
    increment(sources, event.source.ip)
    increment(traps, event.trap_id)
    increment(services, `${event.destination.protocol}/${event.destination.port}`)
    const severity = eventSeverity(event)
    increment(severities, severity)
    if (severity === 'high' || severity === 'critical') highSeverity++
    if (['service.auth_attempt', 'ssh.auth_attempt'].includes(event.event_type)) authAttempts++
    sessions.add(JSON.stringify([event.trap_id, event.session_id]))
    const at = Date.parse(event.occurred_at)
    if (Number.isFinite(at)) timestamps.push(at)

    const code = event.source_enrichment?.country_code
    if (code && countryName(code)) {
      increment(countries, code)
      const ips = countrySources.get(code) ?? new Set<string>()
      ips.add(event.source.ip)
      countrySources.set(code, ips)
      geoKnown++
    }
    const asn = event.source_enrichment?.asn
    if (typeof asn === 'number' && Number.isInteger(asn) && asn > 0 && asn <= 4294967295) {
      increment(asns, String(asn))
      asnKnown++
    }
  }

  const start = timestamps.length ? Math.min(...timestamps) : null
  const end = timestamps.length ? Math.max(...timestamps) : null
  const activity: { at: number; until: number; count: number }[] = []
  if (start !== null && end !== null) {
    const bucketCount = Math.min(24, end - start + 1)
    const width = Math.max(1, Math.ceil((end - start + 1) / bucketCount))
    for (let i = 0; i < bucketCount; i++) activity.push({ at: start + i * width, until: Math.min(end, start + (i + 1) * width - 1), count: 0 })
    for (const at of timestamps) activity[Math.min(bucketCount - 1, Math.floor((at - start) / width))].count++
  }

  return {
    total: events.length, uniqueSources: sources.size, sessions: sessions.size,
    authAttempts, highSeverity, geoKnown, geoUnknown: events.length - geoKnown,
    asnUnknown: events.length - asnKnown, start, end, activity,
    types: rank(types), sources: rank(sources), traps: rank(traps), services: rank(services),
    severities: rank(severities), asns: rank(asns),
    countries: rank(countries).map(row => ({ ...row, sources: countrySources.get(row.key)!.size })),
  }
}
