import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'

test('navigation groups tasks and preserves administrator visibility', async () => {
  const root = fileURLToPath(new URL('../../', import.meta.url))
  const result = await build({
    stdin: { contents: `
      import { createElement as h } from 'react'
      import { renderToStaticMarkup } from 'react-dom/server'
      import { MemoryRouter } from 'react-router-dom'
      import { Navigation } from './src/components/Navigation'
      export const admin = renderToStaticMarkup(h(MemoryRouter, { initialEntries: ['/app'] }, h(Navigation, { admin: true })))
      export const viewer = renderToStaticMarkup(h(MemoryRouter, { initialEntries: ['/app'] }, h(Navigation, { admin: false })))
    `, resolveDir: root },
    bundle: true, write: false, format: 'esm', platform: 'browser', alias: { '@': `${root}/src` },
    define: { 'process.env.NODE_ENV': '"production"' },
  })
  const view = await import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`)
  for (const html of [view.admin, view.viewer]) {
    assert.match(html, /Мониторинг/)
    assert.match(html, /Настройка ловушек/)
    assert.match(html, /aria-current="page"/)
    assert.match(html, /href="\/app\/statistics"/)
    assert.match(html, /Статистика/)
  }
  assert.match(view.admin, /Приглашения/)
  assert.doesNotMatch(view.viewer, /Приглашения|Админ-панель/)
})
