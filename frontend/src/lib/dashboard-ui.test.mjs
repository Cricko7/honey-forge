import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'

async function renderDashboard({ failed = false, loading = false } = {}) {
  const root = fileURLToPath(new URL('../../', import.meta.url))
  const event = (id, type) => ({
    event_id: id, event_type: type, occurred_at: new Date().toISOString(),
    source: { ip: '192.0.2.1', port: 1234 },
  })
  const result = await build({
    stdin: {
      contents: `
        import { createElement as h } from 'react'
        import { renderToStaticMarkup } from 'react-dom/server'
        import { MemoryRouter } from 'react-router-dom'
        import { DashboardPage } from './src/pages/Dashboard'
        export default renderToStaticMarkup(h(MemoryRouter, null, h(DashboardPage)))
      `,
      resolveDir: root,
    },
    bundle: true, write: false, format: 'esm', platform: 'browser',
    alias: { '@': `${root}/src` },
    define: { 'process.env.NODE_ENV': '"production"' },
    plugins: [{
      name: 'dashboard-state',
      setup(builder) {
        builder.onLoad({ filter: /[\\/]api[\\/]hooks\.ts$/ }, () => ({
          loader: 'ts',
          contents: `
            const state = ${JSON.stringify({
              isLoading: loading,
              error: failed ? { message: 'PRIVATE SERVER DETAILS' } : null,
              data: failed || loading ? undefined : { items: [event('auth/id', 'service.auth_attempt'), event('payload', 'service.payload')] },
            })}
            export const useEvents = () => ({ ...state, refetch() {} })
            export const useTraps = () => ({ ...state, data: state.data ? { items: [{ connectivity: 'online', runtime_state: 'stopped' }] } : undefined, refetch() {} })
            export const useLiveStream = () => {}
            export const useCanManage = () => true
          `,
        }))
        builder.onLoad({ filter: /[\\/]lib[\\/]theme\.tsx$/ }, () => ({
          loader: 'ts', contents: 'export const useThemeColors = () => ({ brand: "#66528b", line: "#ddd", muted: "#666", panel: "#fff" })',
        }))
      },
    }],
  })
  return (await import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`)).default
}

test('dashboard identifies sampled data and exposes recent event details without claiming totals', async () => {
  const html = await renderDashboard()
  assert.match(html, /Выборка: последние 100 событий/)
  assert.doesNotMatch(html, /за всё время|перехвачено|LIVE|Активные ловушки/)
  assert.match(html, /На связи/)
  assert.match(html, /href="\/app\/events\?event=auth%2Fid"/)
  assert.match(html, /service\.auth_attempt/)
  assert.match(html, /За 24 часа в выборке/)
  assert.doesNotMatch(html, />События за 24 часа</)
  assert.match(html, /href="\/app\/network\?create=1"/)
})

test('dashboard shows a retryable failure instead of presenting unavailable data as an empty journal', async () => {
  const html = await renderDashboard({ failed: true })
  assert.match(html, /role="alert"/)
  assert.match(html, /Повторить/)
  assert.doesNotMatch(html, /PRIVATE SERVER DETAILS|Событий пока нет/)
})

test('dashboard exposes named activity and journal sections for keyboard and screen reader navigation', async () => {
  const html = await renderDashboard()
  assert.match(html, /<section[^>]*aria-labelledby="activity-title"/)
  assert.match(html, /<h2[^>]*id="activity-title"[^>]*>Активность за 24 часа<\/h2>/)
  assert.match(html, /<section[^>]*aria-labelledby="recent-events-title"/)
  assert.match(html, /<h2[^>]*id="recent-events-title"[^>]*>Последние события<\/h2>/)
})
