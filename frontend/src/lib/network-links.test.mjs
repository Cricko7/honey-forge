import test from 'node:test'
import assert from 'node:assert/strict'
import { canConnectTraps, trapLinkKey } from './network-links.ts'

test('directed trap links allow fan-out and reverse links', () => {
  const traps = [{ id: 'a' }, { id: 'b' }, { id: 'c' }]
  const links = [{ source_id: 'a', target_id: 'b' }]
  assert.equal(canConnectTraps('a', 'c', traps, links), true)
  assert.equal(canConnectTraps('b', 'a', traps, links), true)
  assert.notEqual(trapLinkKey(links[0]), trapLinkKey({ source_id: 'b', target_id: 'a' }))
})

test('rejects duplicates, self links, gateway and missing traps', () => {
  const traps = [{ id: 'a' }, { id: 'b' }]
  const links = [{ source_id: 'a', target_id: 'b' }]
  for (const [source, target] of [['a', 'b'], ['a', 'a'], ['gateway', 'a'], ['a', 'missing'], [null, 'a']]) {
    assert.equal(canConnectTraps(source, target, traps, links), false)
  }
})
