import type { Event } from '@/types'
import { matchesEvent, type EventFilter } from './event-filters'

export function mockEventExport(events: Event[], format: 'csv' | 'stix', filter?: EventFilter): Blob {
  const selected = events.filter((e) => matchesEvent(e, filter))
  if (format === 'csv') {
    const columns = ['event_id', 'event_type', 'trap_id', 'type_id', 'type_version', 'profile_revision', 'occurred_at', 'received_at', 'session_id', 'session_sequence', 'source_ip', 'source_port', 'destination_protocol', 'destination_port', 'source_enrichment', 'data']
    const cell = (value: unknown) => {
      let text = String(value ?? '')
      if (/^[\s]*[=+@-]|^[\t\r\n]/.test(text)) text = `'${text}`
      return `"${text.replaceAll('"', '""')}"`
    }
    const rows = selected.map((e) => [e.event_id, e.event_type, e.trap_id, e.type_id, e.type_version, e.profile_revision, e.occurred_at, e.received_at, e.session_id, e.session_sequence, e.source.ip, e.source.port, e.destination.protocol, e.destination.port, JSON.stringify(e.source_enrichment ?? null), JSON.stringify(e.data)].map(cell).join(','))
    return new Blob([[columns.join(','), ...rows].join('\r\n') + '\r\n'], { type: 'text/csv;charset=utf-8' })
  }
  const objects = selected.flatMap((event) => {
    const type = event.source.ip.includes(':') ? 'ipv6-addr' : 'ipv4-addr'
    const id = `${type}--${crypto.randomUUID()}`
    return [
      { type, spec_version: '2.1', id, value: event.source.ip },
      { type: 'observed-data', spec_version: '2.1', id: `observed-data--${crypto.randomUUID()}`, created: event.received_at, modified: event.received_at, first_observed: event.occurred_at, last_observed: event.occurred_at, number_observed: 1, object_refs: [id], x_honeyforge_event: event },
    ]
  })
  return new Blob([JSON.stringify({ type: 'bundle', id: `bundle--${crypto.randomUUID()}`, objects })], { type: 'application/stix+json' })
}
