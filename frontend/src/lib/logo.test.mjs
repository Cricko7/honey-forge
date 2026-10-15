import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'

const root = fileURLToPath(new URL('../../', import.meta.url))
const result = await build({
  stdin: { contents: `
    import { createElement as h } from 'react'
    import { renderToStaticMarkup } from 'react-dom/server'
    import { Logo } from './src/components/Logo'
    export const logo = (props) => renderToStaticMarkup(h(Logo, props))
  `, resolveDir: root },
  bundle: true, write: false, format: 'esm', platform: 'browser',
  define: { 'process.env.NODE_ENV': '"production"' },
})
const { logo } = await import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`)

test('logo reserves a readable square and remains decorative beside the brand name', () => {
  const html = logo()
  assert.match(html, /width="40" height="40"/)
  assert.match(html, /aria-hidden="true"/)
  assert.match(html, /focusable="false"/)
  assert.doesNotMatch(html, /<image|<rect/)
})

test('logo accepts the caller size and layout class without requiring a theme provider', () => {
  const html = logo({ size: 48, className: 'shrink-0' })
  assert.match(html, /width="48" height="48"/)
  assert.match(html, /class="shrink-0"/)
})
