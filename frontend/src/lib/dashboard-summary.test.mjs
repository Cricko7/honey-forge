import test from 'node:test'
import assert from 'node:assert/strict'
import { summarizeDashboard } from './dashboard-summary.ts'

test('hourly activity stays chronological across midnight and excludes old, future and invalid dates', () => {
  const now = Date.parse('2026-10-15T01:30:00Z')
  const event = (at, type = 'service.connection_opened') => ({
    occurred_at: at, event_type: type, source: { ip: '192.0.2.1' },
  })
  const result = summarizeDashboard([
    event('2026-10-14T01:29:59Z'),
    event('2026-10-14T01:30:00Z'),
    event('2026-10-14T23:50:00Z'),
    event('2026-10-15T01:30:00Z'),
    event('2026-10-15T01:30:01Z'),
    event('invalid'),
  ], now)
  assert.equal(result.events24h, 3)
  assert.equal(result.activity.reduce((sum, bucket) => sum + bucket.count, 0), 3)
  assert.ok(result.activity.every((bucket, index, all) => index === 0 || bucket.at > all[index - 1].at))
  assert.equal(result.activity.at(-1).at, Date.parse('2026-10-15T01:00:00Z'))
})

test('authentication counts exclude payloads and honeytokens, and event type names remain distinct', () => {
  const events = ['service.auth_attempt', 'ssh.auth_attempt', 'service.payload', 'honeytoken.triggered'].map(event_type => ({
    event_type, occurred_at: '2026-10-15T00:00:00Z', source: { ip: '192.0.2.1' },
  }))
  const result = summarizeDashboard(events, Date.parse('2026-10-15T01:00:00Z'))
  assert.equal(result.authAttempts, 2)
  assert.equal(result.byType.length, 4)
  assert.equal(result.sources[0].count, 4)
  assert.deepEqual(summarizeDashboard([], Date.now()).sources, [])
})
