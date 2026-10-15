import test from 'node:test'
import assert from 'node:assert/strict'
import { canConnectTraps, trapLinkKey, trapLinkLabel, trapLinkHandles } from './network-links.ts'

test('connections follow the relative position on all four sides', () => {
  for (const [target, expected] of [
    [{ x: 300, y: 0 }, { sourceHandle: 'right', targetHandle: 'left' }],
    [{ x: -300, y: 0 }, { sourceHandle: 'left', targetHandle: 'right' }],
    [{ x: 0, y: 300 }, { sourceHandle: 'bottom', targetHandle: 'top' }],
    [{ x: 0, y: -300 }, { sourceHandle: 'top', targetHandle: 'bottom' }],
  ]) assert.deepEqual(trapLinkHandles({ x: 0, y: 0 }, target), expected)
})

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

test('network-routed sends use each outgoing HTTP arrow', () => {
  const config = { blocks: [{ kind: 'send', config: { target_id: '00000000-0000-0000-0000-000000000000' } }] }
  assert.equal(trapLinkLabel({ source_id: 'a', target_id: 'b' }, config, 'custom-http'), 'передача JSON')
  assert.equal(trapLinkLabel({ source_id: 'a', target_id: 'c' }, config, 'custom-http'), 'передача JSON')
  assert.equal(trapLinkLabel({ source_id: 'a', target_id: 'redis' }, config, 'redis-emulator'), 'копия трафика')
})

test('rejects duplicates, self links, gateway and missing traps', () => {
  const traps = [{ id: 'a' }, { id: 'b' }]
  const links = [{ source_id: 'a', target_id: 'b' }]
  for (const [source, target] of [['a', 'b'], ['a', 'a'], ['gateway', 'a'], ['a', 'missing'], [null, 'a']]) {
    assert.equal(canConnectTraps(source, target, traps, links), false)
  }
})
