import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'

test('API key documentation is available next to keys and includes a section menu', async () => {
  const root = fileURLToPath(new URL('../../', import.meta.url))
  const result = await build({
    stdin: {
      contents: `
        import { createElement as h } from 'react'
        import { renderToStaticMarkup } from 'react-dom/server'
        import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
        import { APIKeys } from './src/pages/organization/APIKeys.tsx'
        import { APIKeyGuide } from './src/pages/organization/APIKeyGuide.tsx'
        export const keys = renderToStaticMarkup(h(QueryClientProvider, { client: new QueryClient() }, h(APIKeys)))
        export const guide = renderToStaticMarkup(h(APIKeyGuide, { open: true, onClose() {} }))
      `,
      resolveDir: root,
    },
    bundle: true, write: false, format: 'esm', platform: 'browser',
    alias: { '@': `${root}/src` },
    define: { 'import.meta.env.VITE_API_BASE': '""', 'process.env.NODE_ENV': '"production"' },
  })
  const { keys, guide } = await import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`)

  assert.match(keys, /Документация API/)
  assert.match(keys, /Выпуск ключей доступен с подключённым сервером/)
  for (const section of ['Авторизация', 'События и данные', 'Запуск ловушек', 'Realtime', 'Лимиты и ошибки']) {
    assert.ok(guide.includes(section), `missing section: ${section}`)
  }
  assert.match(guide, /Authorization: Bearer/)
  assert.match(guide, /Повторно посмотреть секрет нельзя/)
  assert.match(guide, /Скопировать пример/)
})
