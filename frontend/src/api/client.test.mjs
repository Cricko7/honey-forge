import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'

async function client(base) {
  const result = await build({
    entryPoints: [fileURLToPath(new URL('./client.ts', import.meta.url))],
    bundle: true, write: false, format: 'esm', platform: 'browser',
    alias: { '@': fileURLToPath(new URL('../', import.meta.url)) },
    define: { 'import.meta.env.VITE_API_BASE': JSON.stringify(base) },
  })
  return import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`)
}

test('admin panel reauthentication sends only the account password and uses the browser CSRF session', async () => {
  const { api } = await client('https://operator.example')
  const originalFetch = globalThis.fetch
  const originalWindow = globalThis.window
  const requests = []
  globalThis.window = { location: { origin: 'https://operator.example' } }
  globalThis.fetch = async (url, init) => {
    requests.push({ url, ...init })
    if (init?.method === 'DELETE') return new Response(null, { status: 204 })
    return new Response(JSON.stringify({ csrf_token: 'csrf', expires_at: null }))
  }
  try {
    await api.getSession()
    await api.getAdminAccess()
    assert.equal(requests.at(-1).url, 'https://operator.example/api/admin-session')
    assert.equal(requests.at(-1).cache, 'no-store')
    await api.confirmAdminAccess('test-account-password')
    assert.equal(requests.at(-1).method, 'POST')
    assert.deepEqual(JSON.parse(requests.at(-1).body), { password: 'test-account-password' })
    assert.equal(requests.at(-1).headers['X-CSRF-Token'], 'csrf')
    assert.equal(requests.at(-1).credentials, 'include')
    await api.lockAdminAccess()
    assert.equal(requests.at(-1).method, 'DELETE')
    assert.equal(requests.at(-1).headers['X-CSRF-Token'], 'csrf')
  } finally { globalThis.fetch = originalFetch; globalThis.window = originalWindow }
})

test('member administration scopes requests to the session and uses CSRF for mutations', async () => {
  const { api } = await client('https://operator.example')
  const originalFetch = globalThis.fetch
  const originalWindow = globalThis.window
  const requests = []
  globalThis.window = { location: { origin: 'https://operator.example' } }
  globalThis.fetch = async (url, init) => {
    requests.push({ url, ...init })
    if (init?.method === 'PATCH' || init?.method === 'POST') return new Response(null, { status: 204 })
    return new Response(JSON.stringify({ csrf_token: 'csrf', items: [], creator: null, next_cursor: null }))
  }
  try {
    await api.getSession()
    await api.listMembers('cursor')
    assert.equal(requests.at(-1).url, 'https://operator.example/api/organization/members?limit=50&cursor=cursor')
    assert.equal(requests.at(-1).cache, 'no-store')
    await api.setTrapPermission('member/id', true, 3)
    assert.equal(requests.at(-1).url, 'https://operator.example/api/organization/members/member%2Fid/trap-permission')
    assert.equal(requests.at(-1).method, 'PATCH')
    assert.deepEqual(JSON.parse(requests.at(-1).body), { can_manage_traps: true, expected_revision: 3 })
    assert.equal(requests.at(-1).headers['X-CSRF-Token'], 'csrf')
    assert.equal(requests.at(-1).credentials, 'include')
    await api.sendPresence()
    assert.equal(requests.at(-1).method, 'POST')
    assert.equal(requests.at(-1).url, 'https://operator.example/api/session/presence')
    assert.equal(requests.at(-1).body, undefined)
    assert.equal(requests.at(-1).headers['X-CSRF-Token'], 'csrf')
  } finally { globalThis.fetch = originalFetch; globalThis.window = originalWindow }
})

test('API key management uses authenticated browser requests and sends only the name on creation', async () => {
  const { api } = await client('https://operator.example')
  const originalFetch = globalThis.fetch
  const originalWindow = globalThis.window
  const requests = []
  globalThis.window = { location: { origin: 'https://operator.example' } }
  globalThis.fetch = async (url, init) => {
    requests.push({ url, ...init })
    if (init?.method === 'DELETE') return new Response(null, { status: 204 })
    return new Response(JSON.stringify({ csrf_token: 'csrf', items: [], id: 'key-id', token: 'one-time-secret' }))
  }

  try {
    await api.getSession()
    await api.listAPIKeys()
    assert.equal(requests.at(-1).url, 'https://operator.example/api/api-keys')
    assert.equal(requests.at(-1).credentials, 'include')

    const created = await api.createAPIKey('SOC integration')
    assert.equal(created.token, 'one-time-secret')
    assert.deepEqual(JSON.parse(requests.at(-1).body), { name: 'SOC integration' })
    assert.equal(requests.at(-1).headers['X-CSRF-Token'], 'csrf')

    await api.revokeAPIKey('key-id')
    assert.equal(requests.at(-1).method, 'DELETE')
    assert.equal(requests.at(-1).url, 'https://operator.example/api/api-keys/key-id')
    assert.equal(requests.at(-1).headers['X-CSRF-Token'], 'csrf')
  } finally {
    globalThis.fetch = originalFetch
    globalThis.window = originalWindow
  }
})

test('event filters reach the backend and export returns an authenticated file', async () => {
  const { api } = await client('https://operator.example')
  const originalFetch = globalThis.fetch
  const requests = []
  globalThis.fetch = async (url, init) => {
    requests.push({ url, ...init })
    if (url.includes('/export?')) return new Response('event_id\r\nexample\r\n', { headers: { 'Content-Type': 'text/csv' } })
    return new Response(JSON.stringify({ items: [], next_cursor: null }))
  }
  try {
    const filter = { type_id: 'tcp-banner', event_type: 'tcp.connection_opened', from: '2026-10-13T00:00:00Z', cursor: 'page', limit: 100 }
    await api.listEvents(filter)
    assert.equal(new URL(requests.at(-1).url).searchParams.get('cursor'), 'page')
    const blob = await api.exportEvents('csv', filter)
    const query = new URL(requests.at(-1).url).searchParams
    assert.equal(query.get('type_id'), filter.type_id)
    assert.equal(query.get('event_type'), filter.event_type)
    assert.equal(query.get('from'), filter.from)
    assert.equal(query.has('cursor'), false)
    assert.equal(query.has('limit'), false)
    assert.equal(requests.at(-1).credentials, 'include')
    assert.equal(await blob.text(), 'event_id\r\nexample\r\n')
  } finally {
    globalThis.fetch = originalFetch
  }
})

test('mock journal filters, paginates and exports the complete selection', async () => {
  const { api } = await client('')
  const first = await api.listEvents({ limit: 1 })
  const type = first.items[0].type_id
  const filtered = await api.listEvents({ type_id: type, limit: 1 })
  const next = await api.listEvents({ type_id: type, limit: 1, cursor: filtered.next_cursor })
  assert.notEqual(next.items[0].event_id, filtered.items[0].event_id)
  assert.equal(next.items[0].type_id, type)
  const bundle = JSON.parse(await (await api.exportEvents('stix', { type_id: type, limit: 1 })).text())
  const observations = bundle.objects.filter((object) => object.type === 'observed-data')
  assert.ok(observations.length > 1)
  assert.ok(observations.every((object) => object.x_honeyforge_event.type_id === type))
  const empty = await api.exportEvents('csv', { type_id: 'missing' })
  assert.equal((await empty.text()).trim().split('\r\n').length, 1)
})

test('deployment nodes and selected placement use the REST contract', async () => {
  const { api } = await client('https://operator.example')
  const originalFetch = globalThis.fetch
  const originalWindow = globalThis.window
  const requests = []
  globalThis.window = { location: { origin: 'https://operator.example' } }
  globalThis.fetch = async (url, init) => {
    requests.push({ url, ...init })
    return new Response(JSON.stringify({ csrf_token: 'csrf', items: [] }), { status: 200 })
  }
  try {
    await api.getSession()
    assert.deepEqual((await api.listDeploymentNodes()).items, [])
    assert.equal(requests.at(-1).url, 'https://operator.example/api/traps/deployment-nodes')
    await api.createTrap({ name: 'Company', profile_id: 'profile', deployment_node_id: 'aaaaaaaaaaaaaaaaaaaaaaaaa' })
    assert.equal(JSON.parse(requests.at(-1).body).deployment_node_id, 'aaaaaaaaaaaaaaaaaaaaaaaaa')
    assert.equal(requests.at(-1).headers['X-CSRF-Token'], 'csrf')
  } finally {
    globalThis.fetch = originalFetch
    globalThis.window = originalWindow
  }
})

test('restored cookies recover CSRF and invite registration uses the existing contract', async () => {
  const { api, ApiError } = await client('https://operator.example')
  const originalFetch = globalThis.fetch
  const originalWindow = globalThis.window
  const requests = []
  let status = 200
  let response = { user: { role: 'viewer' }, csrf_token: 'restored-csrf' }
  globalThis.window = { location: { origin: 'https://operator.example' } }
  globalThis.fetch = async (url, init) => {
    requests.push({ url, ...init })
    return new Response(status === 204 ? null : JSON.stringify(response), { status })
  }
  try {
    await api.getSession()
    assert.equal(requests[0].credentials, 'include')
    assert.equal(requests[0].url, 'https://operator.example/api/session')
    await api.register('viewer@example.com', 'long-password', ' code ', 'join')
    assert.deepEqual(JSON.parse(requests.at(-1).body), {
      email: 'viewer@example.com', password: 'long-password', organization: { mode: 'join', join_code: 'code' },
    })
    await api.getJoinCode()
    await api.rotateJoinCode(2)
    assert.equal(requests.at(-1).headers['X-CSRF-Token'], 'restored-csrf')
    assert.equal(requests.at(-1).credentials, 'include')
    assert.deepEqual(JSON.parse(requests.at(-1).body), { expected_revision: 2 })
    assert.equal(requests.at(-1).url, 'https://operator.example/api/organization/join-code/rotations')
    status = 409
    response = { error: { code: 'join_code_changed', message: 'changed' } }
    await assert.rejects(api.rotateJoinCode(2), (err) => err instanceof ApiError && err.code === 'join_code_changed')
    status = 204
    await api.logout()
    status = 200
    await api.login('admin@example.com', 'long-password')
    assert.equal(requests.at(-1).headers['X-CSRF-Token'], undefined)
  } finally {
    globalThis.fetch = originalFetch
    globalThis.window = originalWindow
  }
})

test('demo logout allows signing in again', async () => {
  const { api } = await client('')
  await api.getSession()
  await api.logout()
  await assert.rejects(api.getSession(), (err) => err.status === 401)
  await api.login('demo@example.com', 'demo-password')
  assert.equal((await api.getSession()).user.role, 'admin')
})

test('trap links use authenticated REST and preserve errors', async () => {
  const { api, ApiError } = await client('https://operator.example')
  const originalFetch = globalThis.fetch
  const originalWindow = globalThis.window
  const requests = []
  let status = 200
  let response = { user: { role: 'admin' }, csrf_token: 'link-csrf' }
  globalThis.window = { location: { origin: 'https://operator.example' } }
  globalThis.fetch = async (url, init) => {
    requests.push({ url, ...init })
    return new Response(status === 204 ? null : JSON.stringify(response), { status })
  }
  try {
    await api.getSession()
    response = { items: [] }
    assert.deepEqual(await api.listTrapLinks(), { items: [] })
    const link = { source_id: 'a', target_id: 'b' }
    status = 201; response = link
    assert.deepEqual(await api.createTrapLink(link), link)
    assert.equal(requests.at(-1).headers['X-CSRF-Token'], 'link-csrf')
    assert.equal(requests.at(-1).credentials, 'include')
    assert.deepEqual(JSON.parse(requests.at(-1).body), link)
    status = 409; response = { error: { code: 'trap_link_exists', message: 'Already linked' } }
    await assert.rejects(api.createTrapLink(link), (err) => err instanceof ApiError && err.status === 409)
    status = 204
    await api.deleteTrapLink(link)
    assert.equal(requests.at(-1).method, 'DELETE')
    assert.equal(requests.at(-1).url, 'https://operator.example/api/trap-links/a/b')
  } finally { globalThis.fetch = originalFetch; globalThis.window = originalWindow }
})

test('demo links survive refetch and allow reverse links', async () => {
  const { api } = await client('')
  const { items } = await api.listTraps()
  const link = { source_id: items[0].id, target_id: items[1].id }
  const reverse = { source_id: link.target_id, target_id: link.source_id }
  await api.createTrapLink(link)
  await api.createTrapLink(reverse)
  assert.equal((await api.listTrapLinks()).items.length, 2)
  await assert.rejects(api.createTrapLink(link), (err) => err.status === 409)
  await assert.rejects(api.createTrapLink({ ...link, target_id: link.source_id }), (err) => err.status === 422)
  await api.deleteTrapLink(link)
  assert.deepEqual((await api.listTrapLinks()).items, [reverse])
  await api.deleteTrapLink(reverse)
})

test('demo trap deletion keeps a shared profile until its last binding is removed', async (t) => {
  t.mock.timers.enable({ apis: ['setInterval'] })
  const { api, subscribeStream } = await client('')
  const { items } = await api.listTraps()
  const original = items.find((trap) => items.filter((other) => other.profile_id === trap.profile_id).length === 1)
  assert.ok(original)
  const shared = await api.createTrap({ name: 'Shared', profile_id: original.profile_id })
  const notifications = []
  const stop = subscribeStream((n) => notifications.push(n))
  try {
    await api.deleteTrap(original.id, original.revision)
    assert.ok((await api.listProfiles()).items.some((p) => p.id === original.profile_id))
    assert.equal(notifications.filter((n) => n.kind === 'profile.deleted').length, 0)
    await api.deleteTrap(shared.id, shared.revision)
    assert.ok(!(await api.listProfiles()).items.some((p) => p.id === original.profile_id))
    assert.deepEqual(notifications.filter((n) => n.kind === 'profile.deleted'), [{ kind: 'profile.deleted', profile_id: original.profile_id }])
  } finally { stop() }
})
