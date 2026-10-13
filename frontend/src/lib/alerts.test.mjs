import test from 'node:test'
import assert from 'node:assert/strict'
import { createAlertTracker, alertReducer, actionErrorMessage } from './alerts.ts'

const trap = (version = 1, changes = {}) => ({ id: 't1', name: 'SSH', state_version: version,
  connectivity: 'online', runtime_state: 'running', agent: { buffer_state: 'ok' }, ...changes })
const changed = (t) => ({ kind: 'trap.changed', trap: t })
const event = (id = 'e1') => ({ kind: 'event.created', event: { event_id: id, event_type: 'auth.attempt',
  trap_id: 't1', source: { ip: '192.0.2.1', port: 123 }, destination: { protocol: 'tcp', port: 22 } } })

test('events link to details and replay is silent', () => {
  const tracker = createAlertTracker()
  const [alert] = tracker.accept(event())
  assert.equal(alert.level, 'warning')
  assert.match(alert.message, /192\.0\.2\.1/)
  assert.equal(alert.href, '/app/events?event=e1')
  assert.deepEqual(tracker.accept(event()), [])
})

test('initial snapshots and heartbeat changes are silent; transitions alert', () => {
  const tracker = createAlertTracker()
  tracker.seed([trap()])
  assert.deepEqual(tracker.accept(changed(trap(2))), [])
  const alerts = tracker.accept(changed(trap(3, { connectivity: 'offline', runtime_state: 'error', agent: { buffer_state: 'full' } })))
  assert.equal(alerts.length, 3)
  assert.ok(alerts.some((a) => a.level === 'error'))
  assert.deepEqual(tracker.accept(changed(trap(2))), [])
  const restored = tracker.accept(changed(trap(4)))
  assert.equal(restored.length, 3)
  assert.ok(restored.every((a) => a.level === 'success'))
})

test('snapshot copies survive mutable mock data and late REST cannot roll state back', () => {
  const tracker = createAlertTracker()
  const t = trap()
  assert.deepEqual(tracker.accept(changed(t)), [])
  t.state_version++
  t.connectivity = 'offline'
  assert.equal(tracker.accept(changed(t)).length, 1)
  tracker.seed([trap()])
  assert.deepEqual(tracker.accept(changed(trap(3, { connectivity: 'offline' }))), [])
})

test('newer REST state prevents obsolete replay from producing a false transition', () => {
  const tracker = createAlertTracker()
  tracker.seed([trap()])
  tracker.seed([trap(10, { connectivity: 'offline' })])
  assert.deepEqual(tracker.accept(changed(trap(2, { connectivity: 'offline' }))), [])
  assert.equal(tracker.accept(changed(trap(11))).length, 1)
})

test('deleted traps do not return through stale snapshots; deletion is emitted once', () => {
  const tracker = createAlertTracker()
  tracker.seed([trap()])
  assert.equal(tracker.accept({ kind: 'trap.deleted', trap_id: 't1' }).length, 1)
  assert.deepEqual(tracker.accept({ kind: 'trap.deleted', trap_id: 't1' }), [])
  assert.deepEqual(tracker.accept(changed(trap(2, { connectivity: 'offline' }))), [])
})

test('commands alert once when terminal and never expose result or error contents', () => {
  for (const status of ['queued', 'running', 'succeeded', 'failed', 'expired']) {
    const tracker = createAlertTracker()
    const n = { kind: 'command.changed', trap_id: 't1', command: { id: 'c1', action: 'start', status,
      error: { message: 'SECRET' }, result: { password: 'SECRET' } } }
    const alerts = tracker.accept(n)
    assert.equal(alerts.length, ['queued', 'running'].includes(status) ? 0 : 1, status)
    assert.ok(!JSON.stringify(alerts).includes('SECRET'))
    assert.deepEqual(tracker.accept(n), [])
  }
})

test('history and visible toasts are bounded, duplicates and dismissed alerts stay read', () => {
  let state = []
  for (let i = 0; i < 110; i++) state = alertReducer(state, { type: 'add', alert: { id: String(i), title: 'Event', level: 'info' }, now: i })
  assert.equal(state.length, 100)
  assert.equal(state.filter((a) => a.toast).length, 3)
  assert.equal(state.filter((a) => !a.read).length, 100)
  state = alertReducer(state, { type: 'read-all' })
  assert.ok(state.every((a) => a.read))
  state = alertReducer(state, { type: 'dismiss', id: '109' })
  assert.equal(state[0].toast, false)
  assert.strictEqual(alertReducer(state, { type: 'add', alert: { id: '109' }, now: 120 }), state)
  assert.deepEqual(alertReducer(state, { type: 'clear' }), [])
})

test('action errors give safe messages for expected failures and hide arbitrary causes', () => {
  for (const [status, phrase] of [[401, 'сессия'], [403, 'прав'], [409, 'Обновите'], [422, 'данные'], [500, 'Попробуйте']]) {
    assert.match(actionErrorMessage({ status, message: 'SECRET' }), new RegExp(phrase))
    assert.ok(!actionErrorMessage({ status, message: 'SECRET' }).includes('SECRET'))
  }
  assert.match(actionErrorMessage(new Error('SECRET')), /соединение/)
})

test('a new account starts with fresh deduplication and no history', () => {
  createAlertTracker().accept(event())
  assert.equal(createAlertTracker().accept(event()).length, 1)
  assert.deepEqual(alertReducer([], { type: 'read-all' }), [])
})
