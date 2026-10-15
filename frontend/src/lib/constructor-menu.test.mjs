import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import postcss from 'postcss'

const css = postcss.parse(readFileSync(new URL('../../../constructor/static/css/app.css', import.meta.url), 'utf8'))
function declarations(selector) {
  const values = {}
  css.walkRules(selector, rule => rule.walkDecls(decl => { values[decl.prop] = decl.value }))
  return values
}

test('board menu is anchored below its own switcher and animates without horizontal movement', () => {
  assert.equal(declarations('.chain-switcher').position, 'relative')
  assert.equal(declarations('.dropdown').top, 'calc(100% + 8px)')
  const animation = declarations('.dropdown').animation.split(' ')[0]
  let frames
  css.walkAtRules('keyframes', rule => { if (rule.params === animation) frames = rule.toString() })
  assert.ok(frames, 'menu animation is defined')
  assert.doesNotMatch(frames, /translate\(/, 'centered overlays must not shift the dropdown')
})

test('board menu constrains long labels and scrolls within the available height', () => {
  assert.equal(declarations('#chainName')['text-overflow'], 'ellipsis')
  assert.equal(declarations('.dropdown .dd-item-main')['overflow-wrap'], 'anywhere')
  assert.equal(declarations('.dropdown .dd-actions')['flex-shrink'], '0')
  assert.match(declarations('.dropdown')['max-height'], /100dvh/)
  assert.equal(declarations('.dropdown')['overflow-y'], 'auto')
})
