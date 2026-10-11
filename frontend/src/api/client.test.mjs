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
