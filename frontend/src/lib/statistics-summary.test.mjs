import test from 'node:test'
import assert from 'node:assert/strict'
import { summarizeStatistics, countryName } from './statistics-summary.ts'

const event = (id, overrides = {}) => ({
  event_id: id, event_type: 'service.auth_attempt', trap_id: 'trap-a', session_id: 'session-a',
  source: { ip: '192.0.2.1' }, destination: { protocol: 'tcp', port: 22 },
  occurred_at: '2026-10-15T10:00:00Z', received_at: '2026-10-15T10:00:01Z',
  source_enrichment: { country_code: 'DE', asn: 51167 }, ...overrides,
})

test('all distributions reconcile to the same sample and sessions are scoped to a trap', () => {
  const summary = summarizeStatistics([
    event('1'), event('2', { severity: 'critical', trap_id: 'trap-b' }),
    event('3', { event_type: 'service.payload', source: { ip: '192.0.2.2' }, source_enrichment: null,
      occurred_at: '2026-10-14T10:00:00Z', destination: { protocol: 'udp', port: 53 } }),
  ])
  assert.equal(summary.total, 3)
  assert.equal(summary.uniqueSources, 2)
  assert.equal(summary.sessions, 2)
  assert.equal(summary.traps.length, 2)
  assert.equal(summary.authAttempts, 2)
  assert.equal(summary.highSeverity, 2)
  assert.equal(summary.geoKnown, 2)
  assert.equal(summary.geoUnknown, 1)
  assert.equal(summary.asnUnknown, 1)
  assert.equal(summary.countries[0].key, 'DE')
  assert.equal(summary.countries[0].count, 2)
  assert.equal(summary.countries[0].sources, 1)
  assert.equal(summary.asns[0].key, '51167')
  for (const rows of [summary.types, summary.sources, summary.severities, summary.traps, summary.services]) {
    assert.equal(rows.reduce((sum, row) => sum + row.count, 0), 3)
  }
  assert.equal(summary.activity.reduce((sum, bucket) => sum + bucket.count, 0), 3)
  assert.equal(summary.start, Date.parse('2026-10-14T10:00:00Z'))
  assert.equal(summary.end, Date.parse('2026-10-15T10:00:00Z'))
})

test('empty, single timestamp and invalid timestamps produce finite, bounded charts', () => {
  const empty = summarizeStatistics([])
  assert.equal(empty.start, null)
  assert.equal(empty.total, 0)
  assert.deepEqual(empty.activity, [])
  for (const items of [[event('1')], [event('1'), event('2')], [event('1', { occurred_at: 'invalid' })]]) {
    const summary = summarizeStatistics(items)
    assert.ok(summary.activity.length <= 24)
    assert.ok(summary.activity.every(bucket => Number.isFinite(bucket.at)))
    assert.equal(summary.activity.reduce((sum, bucket) => sum + bucket.count, 0), items.filter(e => e.occurred_at !== 'invalid').length)
  }
})

test('unknown geography and ASN remain explicit; untrusted strings never become object keys', () => {
  assert.equal(countryName('DE'), 'Германия')
  for (const code of ['ZZ', '<script>', '__proto__', 'de', null]) assert.equal(countryName(code), null)
  const summary = summarizeStatistics([
    event('1', { event_type: '__proto__', source: { ip: '<img src=x onerror=alert(1)>' }, source_enrichment: { country_code: '<script>', asn: -1 } }),
    event('2', { source_enrichment: { country_code: 'ZZ', asn: 1.5 } }),
  ])
  assert.equal(summary.geoUnknown, 2)
  assert.equal(summary.asnUnknown, 2)
  assert.deepEqual(summary.countries, [])
  assert.ok(summary.types.some(row => row.key === '__proto__' && row.count === 1))
})

test('ties are deterministic and activity includes both endpoints of a long sample', () => {
  const summary = summarizeStatistics([event('1', { occurred_at: '2020-01-01T00:00:00Z' }), event('2', {
    occurred_at: '2026-10-15T00:00:00Z', source: { ip: '192.0.2.2' },
  })])
  assert.equal(summary.activity.length, 24)
  assert.equal(summary.activity[0].count, 1)
  assert.equal(summary.activity.at(-1).count, 1)
  assert.deepEqual(summary.sources.map(row => row.key), ['192.0.2.1', '192.0.2.2'])
})
