import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'

const root = fileURLToPath(new URL('../../', import.meta.url))

test('server guide contains complete setup and troubleshooting inside the website', async () => {
  const { html } = await compile(`
    import { createElement as h } from 'react'
    import { renderToStaticMarkup } from 'react-dom/server'
    import { ServerGuide } from './src/pages/servers/ServerGuide.tsx'
    export const html = renderToStaticMarkup(h(ServerGuide, { organization: '11111111-1111-4111-8111-111111111111' }))
  `)
  for (const text of ['Подробная инструкция', 'docker swarm join-token worker', 'docker load -i', '2377', '7946', '4789', 'Ready', 'Active', '30 секунд', 'Остановить', 'honeyforge.organization=11111111-1111-4111-8111-111111111111', 'No such image', 'address already in use']) {
    assert.ok(html.includes(text), `missing guide detail: ${text}`)
  }
  assert.match(html, /<details[^>]*>\s*<summary[^>]*>Подробная инструкция/)
  assert.doesNotMatch(html, /<details[^>]*\sopen(?:="")?[^>]*>/)
})

async function compile(contents) {
  const result = await build({
    stdin: { contents, resolveDir: root },
    bundle: true, write: false, format: 'esm', platform: 'browser',
    alias: { '@': `${root}/src` },
    define: { 'process.env.NODE_ENV': '"production"' },
  })
  return import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`)
}

test('server connection commands validate identifiers and reject shell fragments', async () => {
  const { workerJoinCommand, assignServerCommand } = await compile('export * from "./src/lib/server-connection.ts"')
  assert.equal(workerJoinCommand('10.0.0.1:2377', 'SWMTKN-1-secret'), 'docker swarm join --token SWMTKN-1-secret 10.0.0.1:2377')
  assert.equal(workerJoinCommand('10.0.0.1:0', 'SWMTKN-1-secret'), '')
  assert.equal(workerJoinCommand('10.0.0.1:2377;id', 'SWMTKN-1-secret'), '')
  assert.equal(workerJoinCommand('10.0.0.1:2377', 'SWMTKN-1-secret $(id)'), '')
  const node = 'aaaaaaaaaaaaaaaaaaaaaaaaa'
  const org = '11111111-1111-4111-8111-111111111111'
  assert.equal(assignServerCommand(node, org), `docker node update --label-add honeyforge.organization=${org} ${node}`)
  assert.equal(assignServerCommand(`${node};id`, org), '')
  assert.equal(assignServerCommand(node, 'not-an-organization'), '')
})

test('connection form explains enrollment and never pre-fills cluster credentials', async () => {
  const { html } = await compile(`
    import { createElement as h } from 'react'
    import { renderToStaticMarkup } from 'react-dom/server'
    import { MemoryRouter } from 'react-router-dom'
    import { ConnectServerModal } from './src/pages/servers/ConnectServerModal.tsx'
    export const html = renderToStaticMarkup(h(MemoryRouter, {}, h(ConnectServerModal, {
      organization: '11111111-1111-4111-8111-111111111111', nodes: [], onClose() {}, onRefresh() {}
    })))
  `)
  assert.match(html, /1\. Подготовьте сервер/)
  assert.match(html, /2\. Подключите к кластеру/)
  assert.match(html, /3\. Назначьте сервер организации/)
  assert.match(html, /id="swarm-token"[^>]*type="password"[^>]*value=""/)
  assert.match(html, /Проверить подключение/)
  assert.doesNotMatch(html, /Создать ловушку на сервере/)
})
