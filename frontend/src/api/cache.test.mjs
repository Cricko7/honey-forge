import test from 'node:test'
import assert from 'node:assert/strict'
import { QueryClient } from '@tanstack/react-query'
import { applyStreamNotification, replaceSessionCache, removeDeletedTrap } from './cache.ts'

const page = (items) => ({ items, next_cursor: null, stream_cursor: 'cursor' })

test('profile deletion removes its card without removing shared or unrelated profiles', () => {
  const qc = new QueryClient()
  const shared = { id: 'shared' }
  qc.setQueryData(['profiles'], page([{ id: 'deleted' }, shared]))
  applyStreamNotification(qc, { kind: 'profile.deleted', profile_id: 'deleted' })
  assert.deepEqual(qc.getQueryData(['profiles']).items, [shared])
  assert.equal(qc.getQueryState(['profiles']).isInvalidated, true)
  qc.clear()
})

test('live updates refetch cursor pages without losing their boundary event', () => {
  const qc = new QueryClient()
  const key = ['events', { limit: 1 }]
  const old = { event_id: 'boundary', trap_id: 'a' }
  qc.setQueryData(key, { ...page([old]), next_cursor: 'after-boundary' })
  applyStreamNotification(qc, { kind: 'event.created', event: { event_id: 'new', trap_id: 'a' } })
  assert.deepEqual(qc.getQueryData(key).items, [old])
  assert.equal(qc.getQueryState(key).isInvalidated, true)
  qc.clear()
})

test('live events honor type and date filters without changing later pages', () => {
  const qc = new QueryClient()
  const event = { event_id: 'new', trap_id: 'a', type_id: 'tcp-banner', event_type: 'tcp.connection_opened', occurred_at: '2026-10-13T00:00:00Z' }
  const filters = [{ type_id: 'redis-emulator' }, { event_type: 'service.action' }, { from: '2026-10-14T00:00:00Z' }, { to: event.occurred_at }, { cursor: 'next-page' }]
  for (const filter of filters) qc.setQueryData(['events', filter], page([]))
  const matching = { type_id: event.type_id, event_type: event.event_type, from: event.occurred_at }
  qc.setQueryData(['events', matching], page([]))
  applyStreamNotification(qc, { kind: 'event.created', event })
  for (const filter of filters) assert.deepEqual(qc.getQueryData(['events', filter]).items, [])
  assert.deepEqual(qc.getQueryData(['events', matching]).items, [event])
  qc.clear()
})

test('replayed trap snapshots cannot overwrite newer REST state or resurrect deletions', () => {
  const qc = new QueryClient()
  const newer = { id: 'a', state_version: 5, connectivity: 'online' }
  qc.setQueryData(['traps', {}], page([newer]))
  qc.setQueryData(['trap', 'a'], newer)
  applyStreamNotification(qc, { kind: 'trap.changed', trap: { ...newer, state_version: 4, connectivity: 'offline' } })
  assert.deepEqual(qc.getQueryData(['trap', 'a']), newer)
  assert.deepEqual(qc.getQueryData(['traps', {}]).items, [newer])
  applyStreamNotification(qc, { kind: 'trap.deleted', trap_id: 'a' })
  applyStreamNotification(qc, { kind: 'trap.changed', trap: newer })
  assert.equal(qc.getQueryData(['trap', 'a']), null)
  assert.deepEqual(qc.getQueryData(['traps', {}]).items, [])
  qc.clear()
})

test('account change removes previous organization data and pending requests', async () => {
  const qc = new QueryClient()
  qc.setQueryData(['profiles'], page([{ id: 'private-profile' }]))
  const pending = qc.fetchQuery({ queryKey: ['events', {}], queryFn: ({ signal }) =>
    new Promise((resolve) => signal.addEventListener('abort', () => resolve(page([{ event_id: 'old-event' }])))) })
  const session = { user: { id: 'new-user' } }
  await replaceSessionCache(qc, session)
  assert.equal(qc.getQueryData(['profiles']), undefined)
  await pending.catch(() => {})
  assert.equal(qc.getQueryData(['events', {}]), undefined)
  assert.deepEqual(qc.getQueryData(['session']), session)
  await replaceSessionCache(qc)
  assert.equal(qc.getQueryData(['session']), null)
  qc.clear()
})

test('live events respect trap filters, limits and duplicate delivery', () => {
  const qc = new QueryClient()
  const event = { event_id: 'new', trap_id: 'a' }
  qc.setQueryData(['events', { trap_id: 'a', limit: 1 }], page([{ event_id: 'old', trap_id: 'a' }]))
  qc.setQueryData(['events', { trap_id: 'b', limit: 20 }], page([]))
  qc.setQueryData(['events', {}], page([]))
  applyStreamNotification(qc, { kind: 'event.created', event })
  applyStreamNotification(qc, { kind: 'event.created', event })
  assert.deepEqual(qc.getQueryData(['events', { trap_id: 'b', limit: 20 }]).items, [])
  assert.deepEqual(qc.getQueryData(['events', { trap_id: 'a', limit: 1 }]).items, [event])
  assert.deepEqual(qc.getQueryData(['events', {}]).items, [event])
  assert.equal(qc.getQueryData(['events', {}]).stream_cursor, 'cursor')
  qc.clear()
})

test('live trap changes move traps between connectivity and profile filters', () => {
  const qc = new QueryClient()
  const old = { id: 'a', connectivity: 'offline', profile_id: 'old' }
  const trap = { ...old, connectivity: 'online', profile_id: 'new' }
  qc.setQueryData(['traps', { connectivity: 'offline' }], page([old]))
  qc.setQueryData(['traps', { connectivity: 'online' }], page([]))
  qc.setQueryData(['traps', { profile_id: 'old' }], page([old]))
  qc.setQueryData(['traps', {}], page([old]))
  applyStreamNotification(qc, { kind: 'trap.changed', trap })
  assert.deepEqual(qc.getQueryData(['traps', { connectivity: 'offline' }]).items, [])
  assert.deepEqual(qc.getQueryData(['traps', { profile_id: 'old' }]).items, [])
  assert.deepEqual(qc.getQueryData(['traps', { connectivity: 'online' }]).items, [trap])
  assert.deepEqual(qc.getQueryData(['trap', 'a']), trap)
  applyStreamNotification(qc, { kind: 'trap.deleted', trap_id: 'a' })
  assert.deepEqual(qc.getQueryData(['traps', {}]).items, [])
  assert.equal(qc.getQueryData(['trap', 'a']), null)
  qc.clear()
})

test('successful deletion clears detail and every cached list without waiting for the stream', () => {
  const qc = new QueryClient()
  const removed = { id: 'a' }
  const kept = { id: 'b' }
  qc.setQueryData(['trap', 'a'], removed)
  qc.setQueryData(['trap', 'b'], kept)
  qc.setQueryData(['traps', {}], page([removed, kept]))
  qc.setQueryData(['traps', { connectivity: 'online' }], page([removed]))
  removeDeletedTrap(qc, 'a')
  removeDeletedTrap(qc, 'a')
  assert.equal(qc.getQueryData(['trap', 'a']), null)
  assert.deepEqual(qc.getQueryData(['trap', 'b']), kept)
  assert.deepEqual(qc.getQueryData(['traps', {}]).items, [kept])
  assert.deepEqual(qc.getQueryData(['traps', { connectivity: 'online' }]).items, [])
  qc.clear()
})
