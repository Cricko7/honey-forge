import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'

async function renderConstructor(mock) {
  const root = fileURLToPath(new URL('../../', import.meta.url))
  const result = await build({
    stdin: {
      contents: `
        import { createElement as h } from 'react'
        import { renderToStaticMarkup } from 'react-dom/server'
        import { ConstructorPage } from './src/pages/Constructor'
        export default renderToStaticMarkup(h(ConstructorPage))
      `,
      resolveDir: root,
    },
    bundle: true, write: false, format: 'esm', platform: 'browser',
    alias: { '@': `${root}/src` },
    define: { 'import.meta.env.VITE_API_BASE': JSON.stringify(mock ? '' : '/'), 'process.env.NODE_ENV': '"production"' },
    plugins: [{
      name: 'static-theme',
      setup(builder) {
        builder.onLoad({ filter: /[\\/]lib[\\/]theme\.tsx$/ }, () => ({ loader: 'ts', contents: 'export const useTheme = () => ({theme: "light"})' }))
      },
    }],
  })
  return (await import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`)).default
}

test('demo constructor explains its server requirement and keeps the guide available', async () => {
  const html = await renderConstructor(true)
  assert.doesNotMatch(html, /<iframe/)
  assert.match(html, /Конструктор доступен при подключении к серверу/)
  assert.match(html, /Как собрать ловушку/)
})

test('connected constructor embeds only the panel-origin constructor', async () => {
  const html = await renderConstructor(false)
  assert.match(html, /<iframe[^>]*src="\/constructor\/\?embed=1&amp;theme=light"/)
  assert.doesNotMatch(html, /Конструктор доступен при подключении к серверу/)
})
