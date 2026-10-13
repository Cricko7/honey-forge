import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
const compiled = await build({ entryPoints: ['src/api/event-filters.ts'], bundle: true, write: false, format: 'esm', platform: 'node' })
const { eventQuery, matchesEvent, journalFilter } = await import(`data:text/javascript;base64,${Buffer.from(compiled.outputFiles[0].text).toString('base64')}`)

test('export retains filters and removes pagination', () => {
  const q = new URLSearchParams(eventQuery({ trap_id: 'trap', type_id: 'redis-emulator', event_type: 'service.action', from: '2026-10-13T00:00:00Z', limit: 1, cursor: 'old' }, 'csv'))
  assert.equal(q.get('type_id'), 'redis-emulator')
  assert.equal(q.get('format'), 'csv')
  assert.equal(q.has('limit'), false)
  assert.equal(q.has('cursor'), false)
})
test('filters combine and dates are inclusive from, exclusive to', () => {
  const event = { trap_id: 'trap', type_id: 'tcp-banner', event_type: 'tcp.connection', occurred_at: '2026-10-13T00:00:00Z' }
  assert.equal(matchesEvent(event, { from: event.occurred_at, type_id: 'tcp-banner' }), true)
  assert.equal(matchesEvent(event, { to: event.occurred_at }), false)
  assert.equal(matchesEvent(event, { trap_id: 'other' }), false)
})
test('date input is converted to UTC and reversed ranges fail', () => {
  const values = { trap_id: '', type_id: '', event_type: '', from: '2026-10-13T10:00', to: '2026-10-13T11:00' }
  assert.equal(journalFilter(values).from, new Date(values.from).toISOString())
  assert.throws(() => journalFilter({ ...values, to: values.from }))
})
