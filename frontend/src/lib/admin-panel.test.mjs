import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'

async function render(canDelegate) {
  const root = fileURLToPath(new URL('../../', import.meta.url))
  const result = await build({
    stdin: { contents: `
      import { createElement as h } from 'react'
      import { renderToStaticMarkup } from 'react-dom/server'
      import { MemberTable } from './src/pages/organization/MemberTable'
      const member = {id:'viewer',email:'<img src=x onerror=alert(1)>',role:'viewer',can_manage_traps:true,online:true,last_seen_at:null,permission_revision:2,created_at:'2026-10-14T00:00:00Z'}
      export default renderToStaticMarkup(h(MemberTable, {members:[member], creatorID:'owner', canDelegate:${canDelegate}, pending:false, onChange:()=>{}}))
    `, resolveDir: root },
    bundle: true, write: false, format: 'esm', platform: 'browser',
    alias: { '@': `${root}/src` }, define: { 'process.env.NODE_ENV': '"production"' },
  })
  return (await import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`)).default
}

test('member table escapes untrusted values and explains delegated access and presence', async () => {
  const html = await render(true)
  assert.match(html, /&lt;img/)
  assert.doesNotMatch(html, /<img/)
  assert.match(html, /В сети/)
  assert.match(html, /Делегировано/)
  assert.match(html, /Отозвать право/)
})

test('member table hides delegation controls from administrators who are not the creator', async () => {
  assert.doesNotMatch(await render(false), /Отозвать право|Выдать право/)
})
