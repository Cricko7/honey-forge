import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'

test('constructor guide explains a complete first flow and separate-trap delivery', async () => {
  const root = fileURLToPath(new URL('../../', import.meta.url))
  const result = await build({
    stdin: {
      contents: `
        import { createElement as h } from 'react'
        import { renderToStaticMarkup } from 'react-dom/server'
        import { ConstructorGuide } from './src/pages/constructor/ConstructorGuide.tsx'
        export const open = renderToStaticMarkup(h(ConstructorGuide, { open: true, onClose() {} }))
        export const closed = renderToStaticMarkup(h(ConstructorGuide, { open: false, onClose() {} }))
      `,
      resolveDir: root,
    },
    bundle: true, write: false, format: 'esm', platform: 'browser',
    alias: { '@': `${root}/src` },
    define: { 'process.env.NODE_ENV': '"production"' },
  })
  const { open, closed } = await import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`)

  assert.equal(closed, '')
  for (const text of ['Первая HTTP-ловушка', 'Соединения и данные', 'Отдельные ловушки', 'Параллельные ветки', 'Проверка и ошибки', 'TCP-ловушка', 'Новая доска', 'Развернуть', 'Разобрать запрос', 'HTTP-ответ', 'json.action', 'vars.user', 'Получатель', 'PowerShell', 'online']) {
    assert.ok(open.includes(text), `missing guidance: ${text}`)
  }
  assert.match(open, /не ждёт ответа получателя/)
  assert.match(open, /Скопировать команду проверки/)
  assert.match(open, /role="dialog"/)
  assert.doesNotMatch(open, /POSTGRES_|NM_TOKEN|http_pipeline_receipts|docker compose/)
})
