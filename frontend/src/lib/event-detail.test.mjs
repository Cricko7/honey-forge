import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'

test('alert detail links render errors safely instead of an endless loading state', async () => {
  const root = fileURLToPath(new URL('../../', import.meta.url))
  const result = await build({
    stdin: { contents: `
      import { createElement as h } from 'react'
      import { renderToStaticMarkup } from 'react-dom/server'
      import { EventDetailModal } from './src/components/EventDetailModal.tsx'
      export const render = (id) => renderToStaticMarkup(h(EventDetailModal, { id, onClose() {} }))
    `, resolveDir: root },
    bundle: true, write: false, format: 'esm', platform: 'browser',
    alias: { '@': `${root}/src` },
    define: { 'import.meta.env.VITE_API_BASE': '""', 'process.env.NODE_ENV': '"production"' },
    plugins: [{ name: 'event-query-result', setup(builder) {
      builder.onLoad({ filter: /api[\\/]hooks\.ts$/ }, () => ({ contents: `
        export function useEvent(id) {
          if (id === 'loading') return { isLoading: true }
          if (id === 'success' || id === 'unknown-outcome' || id === 'redis-response') return { data: { event_type: 'tcp.connection_opened', occurred_at: '2026-10-13T00:00:00Z',
            source: { ip: '192.0.2.1', port: 123 }, destination: { protocol: 'tcp', port: 22 }, session_sequence: 1,
            source_enrichment: { country_code: null, asn: null }, data: id === 'redis-response' ? { service: 'redis', action_kind: 'request', input: 'Redis response: -WRONGPASS invalid credentials', outcome: 'rejected' } : id === 'unknown-outcome' ? { outcome: 'unknown' } : {}, type_id: 'tcp-banner' } }
          return { isLoading: false, error: { status: Number(id), message: 'SECRET' }, refetch() {} }
        }
      ` }))
    } }],
  })
  const { render } = await import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`)
  assert.match(render('success'), /192\.0\.2\.1/)
  assert.match(render('unknown-outcome'), /Результат не определён/)
  assert.doesNotMatch(render('unknown-outcome'), /unknown/)
  assert.match(render('redis-response'), /Ответ Redis/)
  assert.match(render('redis-response'), /WRONGPASS/)
  assert.match(render('redis-response'), /Отклонено/)
  assert.match(render('loading'), /Загрузка/)
  for (const [status, phrase] of [['404', 'недоступен'], ['403', 'прав'], ['401', 'сессия'], ['500', 'Попробуйте']]) {
    const html = render(status)
    assert.match(html, /role="alert"/)
    assert.match(html, new RegExp(phrase))
    assert.match(html, /Повторить/)
    assert.doesNotMatch(html, /SECRET|Загрузка/)
  }
})
