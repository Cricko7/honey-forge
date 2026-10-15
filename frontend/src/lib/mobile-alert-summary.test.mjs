import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'

test('compact alerts preserve unread count, critical announcements and safe text', async () => {
  const root = fileURLToPath(new URL('../../', import.meta.url))
  const result = await build({
    stdin: {
      contents: `
        import { createElement as h } from 'react'
        import { renderToStaticMarkup } from 'react-dom/server'
        import { MobileAlertSummary } from './src/components/MobileAlertSummary'
        const render = (items) => renderToStaticMarkup(h(MobileAlertSummary, { items, onOpen() {} }))
        export const empty = render([])
        export const critical = render([
          { id: 'new', title: 'Новое событие <img>', level: 'warning', toast: true, read: false },
          { id: 'critical', title: 'Ошибка ловушки', level: 'error', toast: true, read: false },
          { id: 'old', title: 'Старое событие', level: 'info', toast: false, read: false },
          { id: 'read', title: 'Прочитано', level: 'info', toast: false, read: true },
        ])
        export const dismissed = render([{ id: 'old', title: 'Old', level: 'error', toast: false, read: false }])
        export const escaped = render([{ id: 'unsafe', title: '<img>', level: 'warning', toast: true, read: false }])
      `,
      resolveDir: root,
    },
    bundle: true, write: false, format: 'esm', platform: 'browser',
    alias: { '@': `${root}/src` },
    define: { 'process.env.NODE_ENV': '"production"' },
  })
  const view = await import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`)
  assert.equal(view.empty, '')
  assert.equal(view.dismissed, '')
  assert.match(view.critical, /role="alert"/)
  assert.match(view.critical, /Ошибка ловушки/)
  assert.match(view.critical, /3 непрочитанных/)
  assert.match(view.critical, /Открыть историю алертов/)
  assert.match(view.escaped, /&lt;img&gt;/)
  assert.doesNotMatch(view.escaped, /<img>/)
})
