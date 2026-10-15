import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'

async function render(page, { failed = false, missing = false, viewer = false, empty = false } = {}) {
  const root = fileURLToPath(new URL('../../', import.meta.url))
  const trap = { id: 'trap-1', name: 'SSH-приманка', type_id: 'tcp-banner', type_version: 1,
    interaction_level: 'low', connectivity: 'online', runtime_state: 'running', desired_state: 'running',
    revision: 1, state_version: 1, applied_profile_revision: 1, created_at: null, agent: null }
  const profile = { id: 'profile-1', name: 'SSH-профиль', type_id: 'tcp-banner', type_version: 1,
    interaction_level: 'low', revision: 1, secret_fields_set: [], config: { listeners: [{ port: 22 }] } }
  const result = await build({
    stdin: { contents: `
      import { createElement as h } from 'react'
      import { renderToStaticMarkup } from 'react-dom/server'
      import { MemoryRouter } from 'react-router-dom'
      import { ${page}Page as Page } from './src/pages/${page}'
      export default renderToStaticMarkup(h(MemoryRouter, null, h(Page)))
    `, resolveDir: root },
    bundle: true, write: false, format: 'esm', platform: 'browser', alias: { '@': `${root}/src` },
    define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env.VITE_API_BASE': '""' },
    plugins: [{ name: 'feature-state', setup(builder) {
      builder.onLoad({ filter: /[\\/]api[\\/]hooks\.ts$/ }, () => ({ loader: 'ts', contents: `
        const error = ${failed || missing ? JSON.stringify({ status: missing ? 404 : 503, message: 'PRIVATE SERVER DETAILS' }) : 'null'}
        const query = (data) => ({ data: error ? undefined : data, error, isLoading: false, refetch() {} })
        export const useTraps = () => query({ items: ${empty ? '[]' : JSON.stringify([trap])} })
        export const useTrap = () => query(${JSON.stringify(trap)})
        export const useProfiles = () => query({ items: ${empty ? '[]' : JSON.stringify([profile])} })
        export const useEvents = () => query({ items: [] })
        export const useEvent = () => query(undefined)
        export const useCommands = () => query({ items: [] })
        export const useCatalog = () => query({ items: [] })
        export const useCanManage = () => ${!viewer}
        export const useCanManageTraps = () => ${!viewer}
        export const useCreateProfile = () => ({ isPending: false })
        export const useSendCommand = () => ({ isPending: false })
        export const useDeleteTrap = () => ({ isPending: false })
      ` }))
    } }],
  })
  return (await import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`)).default
}

for (const page of ['Traps', 'Profiles', 'TrapDetail', 'Catalog']) {
  test(`${page} distinguishes an unavailable server from empty or missing data`, async () => {
    const html = await render(page, { failed: true })
    assert.match(html, /role="alert"/)
    assert.match(html, /Повторить/)
    assert.doesNotMatch(html, /PRIVATE SERVER DETAILS|Ловушек нет|Профилей нет|Ловушка не найдена/)
  })
}

test('only a missing trap is presented as not found', async () => {
  assert.match(await render('TrapDetail', { missing: true }), /Ловушка не найдена/)
})

test('trap creation is directly discoverable and respects permissions', async () => {
  const html = await render('Traps')
  assert.match(html, /href="\/app\/network\?create=1"[^>]*>.*?Создать ловушку/s)
  assert.doesNotMatch(await render('Traps', { viewer: true }), /Создать ловушку/)
  assert.match(await render('Traps', { empty: true, viewer: true }), /администратор/i)
})

test('trap list keeps runtime status visible on small screens', async () => {
  const html = await render('Traps')
  assert.doesNotMatch(html, /min-w-\[820px\]/)
  assert.match(html, /Работает/)
})

test('profiles explain their main settings and disclose JSON only on request', async () => {
  const html = await render('Profiles')
  assert.match(html, /Порты/)
  assert.match(html, /<details[^>]*>.*?<summary[^>]*>.*?Конфигурация JSON/s)
})
