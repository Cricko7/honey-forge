import test from 'node:test'
import assert from 'node:assert/strict'
import { eventLabel, runtimeLabels, commandStatusLabels, levelLabel } from './ui-labels.ts'

test('known status and event codes have readable, consistent names', () => {
  assert.equal(eventLabel('tcp.payload_received'), 'TCP: получены данные')
  assert.equal(eventLabel('service.auth_attempt'), 'Попытка входа')
  assert.equal(runtimeLabels.running, 'Работает')
  assert.equal(commandStatusLabels.queued, 'В очереди')
  assert.equal(levelLabel('medium'), 'Средний')
})

test('unknown catalog values stay identifiable rather than becoming empty labels', () => {
  assert.equal(eventLabel('future.event'), 'future.event')
  assert.equal(levelLabel('future-level'), 'future-level')
})
