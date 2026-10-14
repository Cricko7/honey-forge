import test from 'node:test'
import assert from 'node:assert/strict'
import { canConnectTraps, trapLinkKey, trapLinkLabel } from './network-links.ts'

test('directed trap links allow fan-out and reverse links', () => {
  const traps = [{ id: 'a' }, { id: 'b' }, { id: 'c' }]
  const links = [{ source_id: 'a', target_id: 'b' }]
  assert.equal(canConnectTraps('a', 'c', traps, links), true)
  assert.equal(canConnectTraps('b', 'a', traps, links), true)
  assert.notEqual(trapLinkKey(links[0]), trapLinkKey({ source_id: 'b', target_id: 'a' }))
})

test('configured HTTP sends are labelled as directed JSON delivery', () => {
  const link = { source_id: 'a', target_id: 'b' }
  const config = { blocks: [{ kind: 'send', config: { target_id: 'b' } }] }
  assert.equal(trapLinkLabel(link, config), 'передача JSON')
  assert.equal(trapLinkLabel({ ...link, target_id: 'c' }, config), 'копия трафика')
  assert.equal(trapLinkLabel(link, undefined), 'копия трафика')
  assert.equal(trapLinkLabel(link, { blocks: [null, { kind: 'send' }] }), 'копия трафика')
})

test('rejects duplicates, self links, gateway and missing traps', () => {
  const traps = [{ id: 'a' }, { id: 'b' }]
  const links = [{ source_id: 'a', target_id: 'b' }]
  for (const [source, target] of [['a', 'b'], ['a', 'a'], ['gateway', 'a'], ['a', 'missing'], [null, 'a']]) {
    assert.equal(canConnectTraps(source, target, traps, links), false)
  }
})
