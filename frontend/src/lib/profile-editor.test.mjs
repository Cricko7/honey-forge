import test from 'node:test'
import assert from 'node:assert/strict'
import { initialProfileConfig, updateEndpoint, profileConfigIssues } from './profile-editor.ts'

test('basic profile changes preserve advanced settings and other listeners', () => {
  const original = { listeners: [{ name: 'ssh', port: 22, banner: 'hello' }, { name: 'ftp', port: 21 }], logging: { capture_payload: false } }
  const next = updateEndpoint(original, 'tcp-banner', { port: 2222 })
  assert.deepEqual(next, { ...original, listeners: [{ ...original.listeners[0], port: 2222 }, original.listeners[1]] })
  assert.equal(original.listeners[0].port, 22)
})

test('port validation reports the field without accepting zero, fractions or overflow', () => {
  for (const port of [0, -1, 65536, 1.5, null, '22']) {
    const errors = profileConfigIssues({ listeners: [{ port }] }, 'tcp-banner')
    assert.match(errors.port, /1.*65535/)
  }
  for (const port of [1, 22, 65535]) {
    assert.deepEqual(profileConfigIssues({ listeners: [{ port }] }, 'tcp-banner'), {})
  }
})

test('profile templates keep type-specific defaults and do not share mutable values', () => {
  const ssh = initialProfileConfig('ssh-high-interaction')
  assert.equal(ssh.services[0].port, 2222)
  assert.equal(typeof ssh.services[0].password, 'string')
  ssh.services[0].port = 9999
  assert.equal(initialProfileConfig('ssh-high-interaction').services[0].port, 2222)
  assert.equal(initialProfileConfig('redis-emulator').services[0].port, 6379)
  assert.ok(initialProfileConfig('honeytoken-http').tokens.length)
})

test('unknown and block-based configurations are left to their own schema validation', () => {
  assert.deepEqual(profileConfigIssues({ blocks: [] }, 'custom-http'), {})
  assert.deepEqual(profileConfigIssues({}, 'future-type'), {})
})
