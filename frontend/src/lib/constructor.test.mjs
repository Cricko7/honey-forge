import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

test('default project launch includes constructor and panel uses its shared origin', () => {
  const entry = readFileSync(new URL('../../../compose.yaml', import.meta.url), 'utf8')
  assert.match(entry, /include:\s*\n\s*- docker-compose.real.yml/)
  const stack = readFileSync(new URL('../../../docker-compose.real.yml', import.meta.url), 'utf8')
  assert.match(stack, /constructor:\s*\n/)
  assert.match(stack, /dockerfile: constructor\/Dockerfile/)
  const page = readFileSync(new URL('../pages/Constructor.tsx', import.meta.url), 'utf8')
  assert.doesNotMatch(page, /127\.0\.0\.1:8077|VITE_CONSTRUCTOR_URL/)
  assert.match(page, /\/constructor\/\?embed=1/)
})

test('constructor requests share panel origin and send session CSRF for mutations', async () => {
  const source = readFileSync(new URL('../../../constructor/static/js/api.js', import.meta.url), 'utf8')
  const originalFetch = globalThis.fetch
  const originalLocation = globalThis.location
  globalThis.location = { pathname: '/constructor/' }
  const calls = []
  globalThis.fetch = async (url, options) => {
    calls.push([url, options])
    return { ok: true, status: 200, json: async () => url === '/api/session' ? { csrf_token: 'session-csrf' } : { id: 'board' } }
  }
  try {
    const { api } = await import('data:text/javascript;base64,' + Buffer.from(source).toString('base64'))
    await api.createChain({ name: 'Custom' })
    assert.equal(calls[0][0], '/api/session')
    assert.equal(calls[1][0], '/constructor/api/chains')
    assert.equal(calls[1][1].headers['X-CSRF-Token'], 'session-csrf')
  } finally {
    globalThis.fetch = originalFetch
    globalThis.location = originalLocation
  }
})
