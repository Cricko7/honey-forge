import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'

async function renderEvents({ failed = false, empty = false, severity = 'low' } = {}) {
  const root = fileURLToPath(new URL('../../', import.meta.url))
  const event = { event_id: 'event-1', event_type: 'service.auth_attempt', severity, occurred_at: '2026-10-15T08:00:00Z',
    source: { ip: '192.0.2.1', port: 5000 }, destination: { protocol: 'redis', port: 6379 }, type_id: 'redis-emulator', type_version: 1 }
  const result = await build({
    stdin: { contents: `
      import { createElement as h } from 'react'
      import { renderToStaticMarkup } from 'react-dom/server'
      import { MemoryRouter } from 'react-router-dom'
      import { EventsPage } from './src/pages/Events'
      export default renderToStaticMarkup(h(MemoryRouter, null, h(EventsPage)))
    `, resolveDir: root },
    bundle: true, write: false, format: 'esm', platform: 'browser', alias: { '@': `${root}/src` },
    define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env.VITE_API_BASE': '""' },
    plugins: [{ name: 'journal-state', setup(builder) {
      builder.onLoad({ filter: /[\\/]api[\\/]hooks\.ts$/ }, () => ({ loader: 'ts', contents: `
        export const useEvents = () => ({ data: ${failed ? 'undefined' : JSON.stringify({ items: empty ? [] : [event] })},
          isLoading: false, error: ${failed ? '{ message: "PRIVATE SERVER DETAILS" }' : 'null'}, refetch() {} })
        export const useTraps = () => ({ data: { items: [] } })
        export const useCatalog = () => ({ data: { items: [] } })
      ` }))
      builder.onLoad({ filter: /[\\/]components[\\/]Alerts\.tsx$/ }, () => ({ loader: 'ts', contents: 'export const useAlerts = () => ({ notify() {} })' }))
      builder.onLoad({ filter: /[\\/]components[\\/]EventDetailModal\.tsx$/ }, () => ({ loader: 'ts', contents: 'export const EventDetailModal = () => null' }))
    } }],
  })
  return (await import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`)).default
}

test('journal preserves all filters, export formats and event detail controls in named groups', async () => {
  const html = await renderEvents()
  assert.match(html, /aria-label="Фильтры событий"/)
  assert.match(html, /aria-label="Экспорт событий"/)
  for (const text of ['Тип ловушки', 'Ловушка', 'Тип события', 'С (включительно)', 'До (не включительно)', 'Скачать CSV', 'Скачать STIX', 'Открыть событие service.auth_attempt', '192.0.2.1', 'redis:6379']) {
    assert.ok(html.includes(text), `Missing journal control or data: ${text}`)
  }
})

test('journal reports one retryable failure without a second empty-state error', async () => {
  const html = await renderEvents({ failed: true })
  assert.match(html, /role="alert"/)
  assert.match(html, /Повторить/)
  assert.doesNotMatch(html, /PRIVATE SERVER DETAILS|Не удалось получить события|Событий нет/)
})

test('empty journal remains distinct from unavailable data', async () => {
  const html = await renderEvents({ empty: true })
  assert.match(html, /Событий нет/)
  assert.doesNotMatch(html, /role="alert"/)
})

test('journal rows and labels expose every severity without relying on color alone', async () => {
  for (const severity of ['low', 'medium', 'high', 'critical']) {
    const html = await renderEvents({ severity })
    assert.ok(html.includes(`data-severity="${severity}"`))
    assert.ok(html.includes(`event-surface-${severity}`))
    assert.ok(html.includes(`event-label-${severity}`))
  }
})
