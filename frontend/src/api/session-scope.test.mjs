import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'
import { QueryClient } from '@tanstack/react-query'
import { sessionDestination } from '../lib/auth.ts'

async function cacheFunctions() {
  const root = fileURLToPath(new URL('../../', import.meta.url))
  const result = await build({
    entryPoints: [`${root}/src/api/session-scope.tsx`],
    bundle: true,
    write: false,
    format: 'esm',
    platform: 'browser',
    define: { 'process.env.NODE_ENV': '"production"' },
  })
  const source = result.outputFiles[0].text
  return import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`)
}

test('late queries, mutations and stream messages remain confined to the previous account', async () => {
  const { createSessionCache, disposeSessionCache } = await cacheFunctions()
  const source = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const previous = createSessionCache(source)
  const next = createSessionCache(source)
  const inviteKey = ['organization', 'join-code']
  const eventKey = ['event', 'a']
  const statisticsKey = ['statistics', 100]
  previous.setQueryData(inviteKey, { code: 'private-a' })
  previous.setQueryData(statisticsKey, { items: [{ event_id: 'private-a' }], hasMore: true })

  let resolveQuery, resolveMutation
  const request = previous.fetchQuery({
    queryKey: eventKey,
    queryFn: () => new Promise(resolve => { resolveQuery = resolve }),
  }).catch(() => {})
  const mutation = previous.getMutationCache().build(previous, {
    mutationFn: () => new Promise(resolve => { resolveMutation = resolve }),
    onSuccess: data => previous.setQueryData(inviteKey, data),
  })
  const action = mutation.execute()
  await Promise.resolve()

  await disposeSessionCache(previous)
  assert.equal(previous.getQueryCache().getAll().length, 0)
  assert.equal(previous.getMutationCache().getAll().length, 0)

  resolveQuery({ payload: 'old-secret' })
  resolveMutation({ code: 'late-secret' })
  await Promise.all([request, action])
  previous.setQueryData(['events'], { items: [{ event_id: 'old-stream' }] })
  assert.equal(next.getQueryData(inviteKey), undefined)
  assert.equal(next.getQueryData(eventKey), undefined)
  assert.equal(previous.getQueryData(statisticsKey), undefined)
  assert.equal(next.getQueryData(statisticsKey), undefined)
  assert.equal(next.getQueryData(['events']), undefined)

  await Promise.all([disposeSessionCache(previous), disposeSessionCache(next)])
  source.clear()
})

test('lost authentication hides private data even when a cached session exists; outages are separate', () => {
  const cached = { user: { id: 'a' }, organization: { id: 'org-a' } }
  for (const status of [401, 403]) {
    assert.equal(sessionDestination(cached, false, { status }), 'guest')
  }
  assert.equal(sessionDestination(cached, false, { status: 503 }), 'error')
})

test('authenticated layout never renders private cache left by another account', async () => {
  const root = fileURLToPath(new URL('../../', import.meta.url))
  const result = await build({
    stdin: {
      contents: `
      import { createElement as h } from 'react'
      import { renderToStaticMarkup } from 'react-dom/server'
      import { MemoryRouter, Routes, Route } from 'react-router-dom'
      import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
      import { Layout } from './src/components/Layout'

      const currentSession = {
        user: { id: 'user-b', email: 'b@example.org', role: 'admin' },
        organization: { id: 'org-b', name: 'Organization B' },
      }
      const inviteKey = ['organization', 'join-code']
      const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
      qc.setQueryData(['session'], currentSession)
      qc.setQueryData(inviteKey, { code: 'PRIVATE-ORG-A-CODE' })

      function PrivatePage() {
        const query = useQuery({
          queryKey: inviteKey,
          queryFn: () => new Promise(() => {}),
          retry: false,
        })
        return h('div', null, query.data?.code ?? 'Checking current organization')
      }

      const route = h(Route, { path: '/app', element: h(Layout) },
        h(Route, { index: true, element: h(PrivatePage) }),
      )
      const app = h(QueryClientProvider, { client: qc },
        h(MemoryRouter, { initialEntries: ['/app'] }, h(Routes, null, route)),
      )
      export default renderToStaticMarkup(app)
    `,
      resolveDir: root,
    },
    bundle: true,
    write: false,
    format: 'esm',
    platform: 'browser',
    alias: { '@': `${root}/src` },
    define: { 'process.env.NODE_ENV': '"production"' },
    plugins: [{
      name: 'session-api',
      setup(builder) {
        builder.onLoad({ filter: /[\\/]components[\\/]ThemeToggle\.tsx$/ }, () => ({
          loader: 'ts',
          contents: 'export const ThemeToggle = () => null',
        }))
        builder.onLoad({ filter: /[\\/]api[\\/]client\.ts$/ }, () => ({
          loader: 'ts',
          contents: `
            export const USE_MOCK = false
            export class ApiError extends Error {}
            export const api = {
              getSession: () => new Promise(() => {}),
              listTraps: () => new Promise(() => {}),
            }
            export const subscribeStream = () => () => {}
          `,
        }))
      },
    }],
  })
  const source = result.outputFiles[0].text
  const { default: html } = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`)
  assert.match(html, /Organization B/)
  assert.equal(html.includes('PRIVATE-ORG-A-CODE'), false)
  assert.match(html, /Checking current organization/)
})
