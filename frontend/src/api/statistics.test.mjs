import test from 'node:test'
import assert from 'node:assert/strict'
import { loadStatisticsSample, parseSampleSize } from './statistics.ts'

const page = (items, next_cursor = null) => ({ items, next_cursor, stream_cursor: 'snapshot' })
const events = (offset, count) => Array.from({ length: count }, (_, i) => ({ event_id: String(offset + i) }))

test('sample size accepts only whole numbers from 1 to 1000', () => {
  for (const value of ['1', '100', '250', '1000']) assert.equal(parseSampleSize(value), Number(value))
  for (const value of ['', '0', '-1', '1.5', '1e3', '1001', 'Infinity', '10<script>', ' 10 ']) {
    assert.equal(parseSampleSize(value), null, value)
  }
})

test('loads exactly N summaries in bounded sequential pages, preserving the server cursor', async () => {
  const calls = []
  const controller = new AbortController()
  const result = await loadStatisticsSample(async (filter, signal) => {
    calls.push({ filter, signal })
    if (!filter.cursor) return page(events(0, 100), 'opaque&cursor')
    if (filter.cursor === 'opaque&cursor') return page(events(100, 100), 'last')
    return page(events(200, 50), 'more')
  }, 250, controller.signal)
  assert.equal(result.items.length, 250)
  assert.equal(result.hasMore, true)
  assert.deepEqual(calls.map(c => c.filter), [
    { limit: 100 }, { limit: 100, cursor: 'opaque&cursor' }, { limit: 50, cursor: 'last' },
  ])
  assert.ok(calls.every(c => c.signal === controller.signal))
})

test('empty and short histories return the actual sample size', async () => {
  assert.deepEqual((await loadStatisticsSample(async () => page([]), 100)).items, [])
  const result = await loadStatisticsSample(async () => page(events(0, 3)), 100)
  assert.equal(result.items.length, 3)
  assert.equal(result.hasMore, false)
})

test('invalid sizes never issue requests', async () => {
  for (const n of [0, -1, 1.5, 1001, NaN, Infinity]) {
    await assert.rejects(loadStatisticsSample(() => assert.fail('unexpected request'), n))
  }
})

test('cancellation and authorization failures stop pagination without returning partial statistics', async () => {
  const controller = new AbortController()
  controller.abort()
  await assert.rejects(loadStatisticsSample(() => assert.fail('aborted request'), 100, controller.signal), { name: 'AbortError' })
  for (const status of [401, 403, 429, 500]) {
    let calls = 0
    const failure = Object.assign(new Error('private details'), { status })
    await assert.rejects(loadStatisticsSample(async () => {
      if (++calls === 1) return page(events(0, 100), 'next')
      throw failure
    }, 250), e => e === failure)
    assert.equal(calls, 2)
  }
})

test('broken pagination cannot loop or silently double count events', async () => {
  let calls = 0
  await assert.rejects(loadStatisticsSample(async () => {
    calls++
    return page(events(0, 100), 'same')
  }, 1000))
  assert.equal(calls, 2)
})

test('the maximum sample needs at most ten requests; short continuation pages fail closed', async () => {
  let calls = 0
  const result = await loadStatisticsSample(async () => {
    const offset = calls++ * 100
    return page(events(offset, 100), `cursor-${calls}`)
  }, 1000)
  assert.equal(calls, 10)
  assert.equal(result.items.length, 1000)
  await assert.rejects(loadStatisticsSample(async () => page(events(0, 1), 'next'), 1000))
})

test('cancellation during a page stops the rest of the snapshot', async () => {
  const controller = new AbortController()
  let calls = 0
  await assert.rejects(loadStatisticsSample(async () => {
    calls++
    controller.abort()
    return page(events(0, 100), 'next')
  }, 1000, controller.signal), { name: 'AbortError' })
  assert.equal(calls, 1)
})
