import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'

async function renderStatistics(state = 'ready') {
  const root = fileURLToPath(new URL('../../', import.meta.url))
  const event = {
    event_id: 'event', event_type: '<script>alert(1)</script>', trap_id: 'trap', session_id: 'session',
    source: { ip: '<img src=x onerror=alert(1)>' }, destination: { protocol: 'tcp', port: 22 },
    occurred_at: '2026-10-15T10:00:00Z', source_enrichment: { country_code: 'DE', asn: 51167 },
  }
  const data = { items: state === 'empty' ? [] : [event], hasMore: false }
  const result = await build({
    stdin: { contents: `
      import { createElement as h } from 'react'
      import { renderToStaticMarkup } from 'react-dom/server'
      import { MemoryRouter } from 'react-router-dom'
      import { StatisticsPage } from './src/pages/Statistics'
      export default renderToStaticMarkup(h(MemoryRouter, null, h(StatisticsPage)))
    `, resolveDir: root },
    bundle: true, write: false, format: 'esm', platform: 'browser',
    alias: { '@': `${root}/src` }, define: { 'process.env.NODE_ENV': '"production"' },
    plugins: [{ name: 'statistics-state', setup(builder) {
      builder.onLoad({ filter: /[\\/]api[\\/]use-statistics\.ts$/ }, () => ({ loader: 'ts', contents: `
        export const useStatistics = () => (${JSON.stringify({
          data: state === 'loading' ? undefined : data,
          isPending: state === 'loading', isFetching: state === 'loading',
          error: state === 'failed' ? { message: 'PRIVATE DATABASE DETAILS' } : state === 'forbidden' ? { status: 403 } : null,
          dataUpdatedAt: Date.parse('2026-10-15T10:00:00Z'),
        })})
      ` }))
      builder.onLoad({ filter: /[\\/]api[\\/]hooks\.ts$/ }, () => ({ loader: 'ts', contents: 'export const useTraps = () => ({ data: { items: [] } })' }))
      builder.onLoad({ filter: /[\\/]lib[\\/]theme\.tsx$/ }, () => ({ loader: 'ts', contents: 'export const useThemeColors = () => ({ brand: "#66528b", line: "#ddd", muted: "#666", panel: "#fff" })' }))
    } }],
  })
  return (await import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`)).default
}

test('statistics exposes sample controls, complete scope and an offline accessible country map', async () => {
  const html = await renderStatistics()
  for (const text of ['Статистика', 'Последние N событий', 'Период выборки', 'География источников', 'ASN', 'Важность', 'Сессии', '1 из 100']) assert.ok(html.includes(text), text)
  assert.match(html, /<label[^>]*for="sample-size"/)
  assert.match(html, /id="sample-size"[^>]*min="1"[^>]*max="1000"/)
  assert.match(html, /<svg[^>]*aria-label="Карта стран источников"/)
  assert.match(html, /Германия/)
  assert.match(html, /Natural Earth/)
  assert.doesNotMatch(html, /<(script|iframe|image)\b|<img src=x|PRIVATE DATABASE DETAILS/)
  assert.match(html, /&lt;script&gt;/)
  assert.match(html, /&lt;img/)
})

test('failed refresh hides statistics instead of displaying stale or partial data', async () => {
  const html = await renderStatistics('failed')
  assert.match(html, /role="alert"/)
  assert.match(html, /Повторить/)
  assert.doesNotMatch(html, /PRIVATE DATABASE DETAILS|Карта стран источников|Германия/)
})

test('access failure never renders previous private statistics', async () => {
  const html = await renderStatistics('forbidden')
  assert.match(html, /role="alert"/)
  assert.doesNotMatch(html, /Карта стран источников|Германия|51167/)
})

test('loading and empty history have distinct states and no misleading zero statistics', async () => {
  assert.match(await renderStatistics('loading'), /Загружаем выборку/)
  const html = await renderStatistics('empty')
  assert.match(html, /Событий пока нет/)
  assert.doesNotMatch(html, /Карта стран источников/)
})
