import test from 'node:test'
import assert from 'node:assert/strict'
import { createNetworkActivity, reconcileNetworkNodes } from './network-state.ts'

test('a burst extends one continuous highlight without extra renders or timers', (t) => {
  t.mock.timers.enable({ apis: ['Date', 'setTimeout'], now: 0 })
  const schedule = t.mock.method(globalThis, 'setTimeout')
  const updates = []
  const activity = createNetworkActivity((active) => updates.push(active))
  activity.accept('a')
  t.mock.timers.tick(1000)
  for (let i = 0; i < 1000; i++) activity.accept('a')
  assert.deepEqual(updates, [{ a: true }])
  assert.equal(schedule.mock.callCount(), 1)
  t.mock.timers.tick(1500)
  assert.equal(updates.length, 1, 'the first deadline must not clear newer activity')
  t.mock.timers.tick(1000)
  assert.deepEqual(updates, [{ a: true }, {}])
  activity.dispose()
})

test('traps expire independently and can highlight again after becoming idle', (t) => {
  t.mock.timers.enable({ apis: ['Date', 'setTimeout'], now: 0 })
  const updates = []
  const activity = createNetworkActivity((active) => updates.push(active))
  activity.accept('a')
  t.mock.timers.tick(1000)
  activity.accept('b')
  t.mock.timers.tick(1500)
  assert.deepEqual(updates.at(-1), { b: true })
  t.mock.timers.tick(1000)
  assert.deepEqual(updates.at(-1), {})
  activity.accept('a')
  assert.deepEqual(updates.at(-1), { a: true })
  activity.dispose()
})

test('leaving the page cancels pending highlights and ignores later events', (t) => {
  t.mock.timers.enable({ apis: ['Date', 'setTimeout'], now: 0 })
  const updates = []
  const activity = createNetworkActivity((active) => updates.push(active))
  activity.accept('a')
  activity.dispose()
  activity.accept('b')
  t.mock.timers.tick(10000)
  assert.deepEqual(updates, [{ a: true }])
})

test('topology refresh preserves dragging, selection and measured dimensions', () => {
  const current = [{ id: 'a', position: { x: 800, y: 500 }, selected: true,
    dragging: true, width: 208, data: { label: 'old' } },
  { id: 'deleted', position: { x: 0, y: 0 }, data: {} }]
  const fresh = [{ id: 'a', position: { x: 0, y: 0 }, data: { label: 'new' } },
    { id: 'b', position: { x: 230, y: 200 }, data: { label: 'added' } }]
  const result = reconcileNetworkNodes(current, fresh)
  assert.deepEqual(result, [{ ...current[0], data: { label: 'new' } }, fresh[1]])
  assert.equal(result[0].position, current[0].position)
  assert.equal(result[1], fresh[1])
  assert.equal(current[0].data.label, 'old')
})

test('explicit layout reset restores default positions and keeps selection', () => {
  const current = [{ id: 'a', position: { x: 800, y: 500 }, selected: true, data: {} }]
  const fresh = [{ id: 'a', position: { x: 120, y: 200 }, data: {} }]
  assert.deepEqual(reconcileNetworkNodes(current, fresh, true), [{ ...fresh[0], selected: true }])
})
