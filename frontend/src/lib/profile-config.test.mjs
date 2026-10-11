import test from 'node:test'
import assert from 'node:assert/strict'
import { parseProfileConfig } from './profile-config.ts'

test('accepts a configuration object', () => {
  assert.deepEqual(parseProfileConfig('{"services":[]}'), { services: [] })
})

test('rejects valid JSON that is not a configuration object', () => {
  for (const value of ['null', '[]', 'true', '42', '"text"']) {
    assert.throws(() => parseProfileConfig(value), /объектом/)
  }
})

test('rejects malformed JSON with a readable message', () => {
  assert.throws(() => parseProfileConfig('{broken'), /корректным JSON/)
})
