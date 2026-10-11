import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'

async function renderPage(page, mock) {
  const root = fileURLToPath(new URL('../../', import.meta.url))
  const result = await build({
    stdin: {
      contents: `
        import { createElement as h } from 'react'
        import { renderToStaticMarkup } from 'react-dom/server'
        import { MemoryRouter } from 'react-router-dom'
        import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
        import { ${page}Page as Page } from './src/pages/${page}.tsx'
        export default renderToStaticMarkup(h(QueryClientProvider, { client: new QueryClient() },
          h(MemoryRouter, {}, h(Page))))
      `,
      resolveDir: root,
    },
    bundle: true, write: false, format: 'esm', platform: 'browser',
    alias: { '@': `${root}/src` },
    define: { 'import.meta.env.VITE_API_BASE': JSON.stringify(mock ? '' : 'https://operator.example'), 'process.env.NODE_ENV': '"production"' },
    plugins: [{
      name: 'omit-theme-toggle',
      setup(builder) {
        builder.onLoad({ filter: /ThemeToggle\.tsx$/ }, () => ({ contents: 'export function ThemeToggle() { return null }' }))
      },
    }],
  })
  const loaded = await import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`)
  return loaded.default
}

for (const page of ['Login', 'Register']) {
  test(`${page} requires explicit credentials for a real account`, async () => {
    const html = await renderPage(page, false)
    assert.match(html, /<input[^>]*type="email"[^>]*value=""/)
    assert.match(html, /<input[^>]*type="password"[^>]*value=""/)
  })

  test(`${page} keeps the prefilled mock demo credentials`, async () => {
    const html = await renderPage(page, true)
    assert.match(html, /<input[^>]*type="email"[^>]*value="admin@perimetr\.ru"/)
    assert.match(html, /<input[^>]*type="password"[^>]*value="demo-password-2026"/)
  })
}
