import test from 'node:test'
import assert from 'node:assert/strict'
import { mkdtemp, writeFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { request } from 'node:http'
import { createServer } from 'vite'

function get(port, path, headers = {}) {
  return new Promise((resolve, reject) => {
    request({ hostname: '127.0.0.1', port, path, headers }, (response) => {
      let body = ''
      response.setEncoding('utf8')
      response.on('data', (chunk) => { body += chunk })
      response.on('end', () => resolve({ status: response.statusCode, headers: response.headers, body }))
    }).on('error', reject).end()
  })
}

test('Vite does not expose local source or environment fixtures to attacker origins and hosts', async () => {
  const root = await mkdtemp(join(tmpdir(), 'honeyforge-vite-security-'))
  let server
  try {
    await writeFile(join(root, 'fixture.txt'), 'public fixture')
    await writeFile(join(root, '.env'), 'TEST_MARKER=private-fixture-marker')
    server = await createServer({ configFile: false, root, logLevel: 'silent', server: { host: '127.0.0.1', port: 0 } })
    await server.listen()
    const port = server.httpServer.address().port
    const source = await get(port, '/fixture.txt', { Origin: 'https://attacker.example' })
    assert.equal(source.status, 200)
    assert.equal(source.headers['access-control-allow-origin'], undefined)
    assert.equal((await get(port, '/fixture.txt', { Host: 'attacker.example' })).status, 403)
    for (const path of ['/.env', '/.env?raw??', '/.env::$DATA?raw']) {
      assert.ok(!(await get(port, path)).body.includes('private-fixture-marker'), `${path} must not reveal environment data`)
    }
  } finally {
    await server?.close()
    await rm(root, { recursive: true, force: true })
  }
})
