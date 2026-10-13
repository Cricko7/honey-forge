import test from 'node:test'
import assert from 'node:assert/strict'
import { subscribeDashboardStream } from './stream.ts'

const flush = async () => { for (let i = 0; i < 12; i++) await Promise.resolve() }

function setup(t) {
  const sockets = []
  class Socket {
    static CONNECTING = 0
    static OPEN = 1
    readyState = 0
    sent = []
    constructor(url, protocol) { this.url = url; this.protocol = protocol; sockets.push(this) }
    open() { this.readyState = 1; this.onopen?.() }
    send(raw) { assert.equal(this.readyState, 1); this.sent.push(JSON.parse(raw)) }
    message(type, payload) { this.onmessage?.({ data: JSON.stringify({ type, payload }) }) }
    close(code = 1000) { this.readyState = 3; this.onclose?.({ code }) }
  }
  const originalSocket = globalThis.WebSocket
  globalThis.WebSocket = Socket
  t.after(() => { globalThis.WebSocket = originalSocket })
  const originalWindow = globalThis.window
  globalThis.window = { location: { origin: 'https://operator.example' } }
  t.after(() => { globalThis.window = originalWindow })
  t.mock.timers.enable({ apis: ['setTimeout'] })
  let syncs = 0
  let checks = 0
  let unauthorized = 0
  const notifications = []
  const options = {
    synchronize: async () => `rest-${++syncs}`,
    checkSession: async () => { checks++ },
    onUnauthorized: () => { unauthorized++ },
  }
  const subscribe = (base = 'https://operator.example/') => {
    const stop = subscribeDashboardStream(base, (n) => notifications.push(n), options)
    t.after(stop)
    return stop
  }
  return { sockets, options, subscribe, notifications, syncs: () => syncs, checks: () => checks, unauthorized: () => unauthorized }
}

test('StrictMode cleanup before initialization creates no aborted handshake', async (t) => {
  const s = setup(t)
  s.subscribe()()
  await flush()
  assert.equal(s.sockets.length, 0)
  assert.equal(s.syncs(), 0)
  s.subscribe()
  await flush()
  assert.equal(s.sockets.length, 1)
  const ws = s.sockets[0]
  assert.equal(ws.url, 'wss://operator.example/api/stream')
  assert.equal(ws.protocol, 'dashboard-stream.v1')
  ws.open()
  assert.deepEqual(ws.sent[0].payload, { after: 'rest-1' })
  assert.equal(ws.sent[0].type, 'stream.subscribe')
})

test('profile deletion reaches the cache consumer', async (t) => {
  const s = setup(t)
  s.subscribe()
  await flush()
  const ws = s.sockets[0]
  ws.open()
  ws.message('profile.deleted', { data: { profile_id: 'deleted-profile' }, cursor: 'after-delete' })
  assert.deepEqual(s.notifications, [{ kind: 'profile.deleted', profile_id: 'deleted-profile' }])
})

test('network reconnect resumes after applied notifications including unknown types', async (t) => {
  const s = setup(t)
  s.subscribe()
  await flush()
  const first = s.sockets[0]
  first.open()
  first.message('stream.ready', { cursor: 'ready' })
  first.message('trap.deleted', { cursor: 'deleted', data: { trap_id: 'trap' } })
  first.message('future.notification', { cursor: 'unknown', data: {} })
  assert.deepEqual(s.notifications, [{ kind: 'trap.deleted', trap_id: 'trap' }])
  first.close(1006)
  t.mock.timers.tick(1000)
  await flush()
  assert.equal(s.sockets.length, 2)
  assert.equal(s.syncs(), 1)
  s.sockets[1].open()
  assert.equal(s.sockets[1].sent[0].payload.after, 'unknown')
  assert.equal(s.checks(), 2)
})

test('expired cursor reloads REST before reconnecting', async (t) => {
  const s = setup(t)
  s.subscribe()
  await flush()
  s.sockets[0].open()
  s.sockets[0].message('error', { error: { code: 'cursor_expired' } })
  s.sockets[0].close(4400)
  t.mock.timers.tick(1000)
  await flush()
  assert.equal(s.syncs(), 2)
  s.sockets[1].open()
  assert.equal(s.sockets[1].sent[0].payload.after, 'rest-2')
})

test('unauthorized close and rejected handshake stop retries', async (t) => {
  const s = setup(t)
  s.subscribe()
  await flush()
  s.sockets[0].close(4401)
  await flush()
  t.mock.timers.tick(60000)
  await flush()
  assert.equal(s.sockets.length, 1)
  assert.equal(s.unauthorized(), 1)
  s.options.checkSession = async () => { throw { status: 401 } }
  s.subscribe()
  await flush()
  assert.equal(s.sockets.length, 1)
  assert.equal(s.unauthorized(), 2)
})

test('unsubscribe cancels pending retries and in-flight REST initialization', async (t) => {
  const s = setup(t)
  const stop = s.subscribe()
  await flush()
  s.sockets[0].close(1013)
  stop()
  t.mock.timers.tick(60000)
  await flush()
  assert.equal(s.sockets.length, 1)
  let resolve
  s.options.synchronize = () => new Promise((r) => { resolve = r })
  const stopPending = s.subscribe()
  await flush()
  stopPending()
  resolve('late')
  await flush()
  assert.equal(s.sockets.length, 1)
})

test('failed local application replays from the previous cursor', async (t) => {
  const s = setup(t)
  const stop = subscribeDashboardStream('https://operator.example', () => { throw new Error('cache failure') }, s.options)
  t.after(stop)
  await flush()
  s.sockets[0].open()
  s.sockets[0].message('stream.ready', { cursor: 'ready' })
  s.sockets[0].message('trap.deleted', { cursor: 'unapplied', data: { trap_id: 'trap' } })
  t.mock.timers.tick(1000)
  await flush()
  s.sockets[1].open()
  assert.equal(s.sockets[1].sent[0].payload.after, 'ready')
})

test('repeated slow consumers resynchronize; protocol violations stop', async (t) => {
  const s = setup(t)
  s.subscribe()
  await flush()
  s.sockets[0].close(4410)
  t.mock.timers.tick(1000)
  await flush()
  assert.equal(s.syncs(), 1)
  s.sockets[1].close(4410)
  t.mock.timers.tick(2000)
  await flush()
  assert.equal(s.syncs(), 2)
  s.sockets[2].close(4400)
  t.mock.timers.tick(60000)
  await flush()
  assert.equal(s.sockets.length, 3)
})

test('a transient handshake outage backs off and cleanup while connecting waits for open', async (t) => {
  const s = setup(t)
  const stop = s.subscribe()
  await flush()
  s.sockets[0].close(1006)
  t.mock.timers.tick(1000)
  await flush()
  s.sockets[1].close(1006)
  t.mock.timers.tick(1999)
  await flush()
  assert.equal(s.sockets.length, 2)
  t.mock.timers.tick(1)
  await flush()
  assert.equal(s.sockets.length, 3)
  stop()
  assert.equal(s.sockets[2].readyState, 0)
  s.sockets[2].open()
  assert.equal(s.sockets[2].readyState, 3)
  assert.deepEqual(s.sockets[2].sent, [])
})
