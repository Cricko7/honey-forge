import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const source = readFileSync(new URL('../../../constructor/static/js/toast.js', import.meta.url), 'utf8')
const { toast } = await import('data:text/javascript;base64,' + Buffer.from(source).toString('base64'))
const escape = value => String(value).replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;').replaceAll('"', '&quot;')

// Record browser text/HTML sinks and serialize their resulting DOM separately.
class Element {
  children = []
  className = ''
  text = ''
  rawHTML = null
  constructor(tagName) { this.tagName = tagName }
  set textContent(value) { this.text = String(value); this.rawHTML = null; this.children = [] }
  get textContent() { return this.text + this.children.map(child => child.textContent).join('') }
  set innerHTML(value) { this.rawHTML = String(value); this.text = String(value).replace(/<[^>]*>/g, ''); this.children = [] }
  get innerHTML() { return this.rawHTML ?? escape(this.text) + this.children.map(child => `<${child.tagName}${child.className ? ` class="${escape(child.className)}"` : ''}>${child.innerHTML}</${child.tagName}>`).join('') }
  appendChild(child) { this.children.push(child) }
  classList = {
    remove: token => { this.className = this.className.split(' ').filter(value => value !== token).join(' ') },
    add: token => { this.className += ' ' + token },
  }
}

function withDOM(run) {
  const originals = { document: globalThis.document, setTimeout: globalThis.setTimeout, clearTimeout: globalThis.clearTimeout }
  const element = new Element('div')
  const timers = [], cleared = []
  globalThis.document = { getElementById: () => element, createElement: name => new Element(name) }
  globalThis.setTimeout = (callback, delay) => { timers.push({ callback, delay }); return timers.length }
  globalThis.clearTimeout = id => { cleared.push(id) }
  try { run(element, timers, cleared) }
  finally { Object.assign(globalThis, originals); delete toast._t }
}

test('constructor toast renders stored labels and backend errors as text', () => {
  withDOM(element => {
    const label = '<img src=x onerror="alert(1)"> & <svg onload="alert(2)">'
    const message = `Ловушка «${label}» создана на дашборде`
    toast(message, 'ok')
    assert.equal(element.textContent, message)
    assert.equal(element.innerHTML.includes('<img'), false)
    assert.equal(element.innerHTML.includes('<svg'), false)
    assert.equal(element.innerHTML.includes('&lt;img'), true)
    toast('Ошибка команды: ' + label, 'err')
    assert.equal(element.textContent, 'Ошибка команды: ' + label)
    assert.equal(element.innerHTML.includes('<img'), false)
  })
})

test('constructor toast builds a real list while escaping every issue field', () => {
  withDOM((element, timers) => {
    const issues = ['[error] <img src=x onerror=alert(1)>', '[<svg onload=alert(2)>] A & B']
    toast('Найдены ошибки:', 'err', issues)
    assert.equal(element.children.length, 1)
    const list = element.children[0]
    assert.equal(list.tagName, 'ul')
    assert.equal(list.className, 'toast-list')
    assert.deepEqual(list.children.map(item => item.tagName), ['li', 'li'])
    assert.deepEqual(list.children.map(item => item.textContent), issues)
    assert.equal(element.innerHTML.includes('<img'), false)
    assert.equal(element.innerHTML.includes('<svg'), false)
    assert.equal(timers.at(-1).delay, 6000)
  })
})

test('a new toast replaces the previous issue list and restarts its visibility timer', () => {
  withDOM((element, timers, cleared) => {
    toast('Предупреждения:', '', ['[warning] проверьте узел'])
    toast('Синхронизировано с дашбордом', 'ok')
    assert.equal(element.textContent, 'Синхронизировано с дашбордом')
    assert.deepEqual(element.children, [])
    assert.equal(element.className, 'toast ok')
    assert.equal(cleared.at(-1), 1)
    assert.equal(timers.at(-1).delay, 2600)
    timers.at(-1).callback()
    assert.equal(element.className, 'toast ok hidden')
  })
})
