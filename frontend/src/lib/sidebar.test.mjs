import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'

const root = fileURLToPath(new URL('../../', import.meta.url))
const result = await build({
  stdin: { contents: `
    import { createElement as h } from 'react'
    import { renderToStaticMarkup } from 'react-dom/server'
    import { MemoryRouter } from 'react-router-dom'
    import { Sidebar } from './src/components/Sidebar'
    import { Navigation } from './src/components/Navigation'
    import { SupportPage } from './src/pages/Support'
    import { UpdatesPage } from './src/pages/Updates'
    export const sidebar = () => renderToStaticMarkup(h(MemoryRouter, null, h(Sidebar, {admin: false}, () => null)))
    export const navigation = (admin, collapsed, path) => renderToStaticMarkup(h(MemoryRouter, {initialEntries: [path]}, h(Navigation, {admin, collapsed})))
    export const support = () => renderToStaticMarkup(h(SupportPage))
    export const updates = () => renderToStaticMarkup(h(UpdatesPage))
  `, resolveDir: root },
  bundle: true, write: false, format: 'esm', platform: 'browser', loader: { '.css': 'empty' },
  alias: { '@': `${root}/src` }, define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env.VITE_API_BASE': '""' },
})
const view = await import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`)

test('help sections are accessible to every role, including collapsed navigation', () => {
  for (const admin of [true, false]) for (const collapsed of [true, false]) {
    for (const [path, label] of [['/app/updates', 'История обновлений'], ['/app/support', 'Техподдержка']]) {
      const html = view.navigation(admin, collapsed, path)
      assert.match(html, new RegExp(`href="${path}"`))
      assert.match(html, new RegExp(label))
      assert.match(html, /aria-current="page"/)
      if (collapsed) assert.match(html, new RegExp(`title="${label}"`))
      if (!admin) assert.doesNotMatch(html, /Админ-панель|Приглашения/)
    }
  }
})

test('sidebar restores only its boolean preference and tolerates unavailable storage', () => {
  const previous = globalThis.localStorage
  try {
    for (const [saved, collapsed] of [['true', true], ['false', false], ['garbage', false], [null, false]]) {
      globalThis.localStorage = { getItem(key) { assert.equal(key, 'honeyforge.sidebar.collapsed'); return saved } }
      const html = view.sidebar()
      assert.match(html, new RegExp(`aria-expanded="${!collapsed}"`))
      assert.match(html, /aria-controls="desktop-navigation"/)
      assert.match(html, new RegExp(collapsed ? 'Развернуть панель' : 'Свернуть панель'))
    }
    globalThis.localStorage = { getItem() { throw new Error('Storage blocked') } }
    assert.match(view.sidebar(), /aria-expanded="true"/)
    delete globalThis.localStorage
    assert.match(view.sidebar(), /aria-expanded="true"/)
  } finally {
    if (previous === undefined) delete globalThis.localStorage
    else globalThis.localStorage = previous
  }
})

test('support provides a bounded email form and a safe Telegram link', () => {
  const html = view.support()
  assert.match(html, /<form/)
  for (const name of ['email', 'subject', 'message']) assert.match(html, new RegExp(`name="${name}"`))
  assert.match(html, /type="email"/)
  assert.match(html, /maxLength="5000"/)
  assert.match(html, /Отправить обращение/)
  assert.match(html, /href="https:\/\/t\.me\/giraffick"[^>]*target="_blank"[^>]*rel="noopener noreferrer"/)
  assert.match(html, /@giraffick/)
  assert.match(html, /Не отправляйте.*пароли.*токены.*API-ключи/)
  assert.doesNotMatch(html, /<iframe|<script|mailto:|https:\/\/t\.me\/giraffick\?/)
})

test('updates present dated product changes rather than operator activity', () => {
  const html = view.updates()
  assert.match(html, /<h1[^>]*>История обновлений<\/h1>/)
  assert.match(html, /<time dateTime="2026-10-15"/)
  assert.match(html, /Техподдержка/)
  assert.match(html, /2026-10-14/)
})
