import test from 'node:test'
import assert from 'node:assert/strict'
import { QueryClient } from '@tanstack/react-query'
import { subscribeMutationAlerts } from './alert-mutations.ts'

test('successful actions notify once and do not include response secrets', async () => {
  const qc = new QueryClient()
  const alerts = []
  const stop = subscribeMutationAlerts(qc.getMutationCache(), (a) => alerts.push(a))
  const mutation = qc.getMutationCache().build(qc, { mutationFn: async () => ({ password: 'SECRET' }),
    meta: { alertSuccess: 'Профиль создан', alertError: 'Не удалось создать профиль' } })
  await mutation.execute({ token: 'SECRET' })
  assert.equal(alerts.length, 1)
  assert.equal(alerts[0].title, 'Профиль создан')
  assert.ok(!JSON.stringify(alerts).includes('SECRET'))
  stop()
})

test('expected failures notify safely while unrelated mutations stay silent', async () => {
  const qc = new QueryClient()
  const alerts = []
  const stop = subscribeMutationAlerts(qc.getMutationCache(), (a) => alerts.push(a))
  for (const meta of [undefined, { alertError: 'Нет доступа' }]) {
    const mutation = qc.getMutationCache().build(qc, { mutationFn: async () => { throw { status: 403, message: 'SECRET' } }, meta })
    await assert.rejects(mutation.execute())
  }
  assert.equal(alerts.length, 1)
  assert.equal(alerts[0].level, 'error')
  assert.match(alerts[0].message, /прав/)
  stop()
})

test('cleared sessions and previous-account actions cannot notify the next account', async () => {
  const qc = new QueryClient()
  const oldAlerts = [], nextAlerts = []
  let resolve
  const stop = subscribeMutationAlerts(qc.getMutationCache(), (a) => oldAlerts.push(a))
  const mutation = qc.getMutationCache().build(qc, { mutationFn: () => new Promise((r) => { resolve = r }), meta: { alertSuccess: 'Готово' } })
  const pending = mutation.execute()
  await Promise.resolve()
  await Promise.resolve()
  stop()
  qc.clear()
  const stopNext = subscribeMutationAlerts(qc.getMutationCache(), (a) => nextAlerts.push(a))
  resolve()
  await pending
  assert.deepEqual(oldAlerts, [])
  assert.deepEqual(nextAlerts, [])
  stopNext()
})

test('removing a pending mutation silences its eventual completion', async () => {
  const qc = new QueryClient()
  const alerts = []
  let resolve
  const stop = subscribeMutationAlerts(qc.getMutationCache(), (a) => alerts.push(a))
  const mutation = qc.getMutationCache().build(qc, { mutationFn: () => new Promise((r) => { resolve = r }), meta: { alertSuccess: 'Готово' } })
  const pending = mutation.execute()
  await Promise.resolve()
  await Promise.resolve()
  qc.clear()
  resolve()
  await pending
  assert.deepEqual(alerts, [])
  stop()
})

test('trap deletion uses the stream identity so action and live delivery share one alert', async () => {
  const qc = new QueryClient()
  const alerts = []
  const stop = subscribeMutationAlerts(qc.getMutationCache(), (a) => alerts.push(a))
  const mutation = qc.getMutationCache().build(qc, { mutationFn: async () => {},
    meta: { alertSuccess: 'Ловушка удалена', alertTrapDeletion: true } })
  await mutation.execute({ id: 't1', revision: 1 })
  assert.equal(alerts[0].id, 'deleted:t1')
  stop()
})
