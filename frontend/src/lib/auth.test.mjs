import test from 'node:test'
import assert from 'node:assert/strict'
import { registrationOrganization, sessionDestination, canManage } from './auth.ts'

test('registration joins by code without accepting an administrator role', () => {
  assert.deepEqual(registrationOrganization('join', '  invite  '), { mode: 'join', join_code: 'invite' })
  assert.deepEqual(registrationOrganization('create', ' Demo '), { mode: 'create', name: 'Demo' })
})

test('only an administrator can change organization data', () => {
  for (const role of ['viewer', 'user', '', undefined]) assert.equal(canManage({ user: { role } }), false)
  assert.equal(canManage(undefined), false)
  assert.equal(canManage({ user: { role: 'admin' } }), true)
})

test('entry restores a session and does not mistake network failures for logout', () => {
  assert.equal(sessionDestination(undefined, true, null), 'loading')
  assert.equal(sessionDestination({ user: { role: 'viewer' } }, false, null), 'app')
  assert.equal(sessionDestination(undefined, false, { status: 401 }), 'guest')
  assert.equal(sessionDestination(null, false, null), 'guest')
  assert.equal(sessionDestination(undefined, false, { status: 503 }), 'error')
})
