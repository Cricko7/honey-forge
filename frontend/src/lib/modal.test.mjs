import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'

test('open dialogs have a unique accessible title and closed dialogs expose no content', async () => {
  const root = fileURLToPath(new URL('../../', import.meta.url))
  const result = await build({
    stdin: {
      contents: `
        import { createElement as h } from 'react'
        import { renderToStaticMarkup } from 'react-dom/server'
        import { Modal } from './src/components/Modal'
        export const open = renderToStaticMarkup(h('div', null,
          h(Modal, {open: true, title: 'Настройки <img>', onClose() {}}, 'Первая форма'),
          h(Modal, {open: true, title: 'Подробности', onClose() {}}, 'Вторая форма')))
        export const closed = renderToStaticMarkup(h(Modal, {open: false, title: 'Скрытая форма', onClose() {}}, 'SECRET'))
      `,
      resolveDir: root,
    },
    bundle: true, write: false, format: 'esm', platform: 'browser',
    define: { 'process.env.NODE_ENV': '"production"' },
  })
  const { open, closed } = await import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`)
  assert.equal(closed, '')
  const titles = [...open.matchAll(/aria-labelledby="([^"]+)"/g)].map(match => match[1])
  assert.equal(titles.length, 2)
  assert.equal(new Set(titles).size, 2)
  for (const title of titles) assert.ok(open.includes(`id="${title}"`))
  assert.equal((open.match(/role="dialog"/g) ?? []).length, 2)
  assert.match(open, /Настройки &lt;img&gt;/)
  assert.doesNotMatch(open, /<img>/)
})
