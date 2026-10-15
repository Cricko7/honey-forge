import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'

async function render(canDelegate, role = 'viewer', pending = false) {
  const root = fileURLToPath(new URL('../../', import.meta.url))
  const result = await build({
    stdin: { contents: `
      import { createElement as h } from 'react'
      import { renderToStaticMarkup } from 'react-dom/server'
      import { MemberTable } from './src/pages/organization/MemberTable'
      const member = {id:'viewer',email:'<img src=x onerror=alert(1)>',role:'${role}',can_manage_traps:true,online:true,last_seen_at:null,permission_revision:2,created_at:'2026-10-14T00:00:00Z'}
      export default renderToStaticMarkup(h(MemberTable, {members:[member], creatorID:'owner', canDelegate:${canDelegate}, pending:${pending}, onChange:()=>{}, onRemove:()=>{}}))
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

test('only the creator sees member removal, administrator targets stay protected', async () => {
  assert.match(await render(true), /Удалить из организации/)
  assert.doesNotMatch(await render(false), /Удалить из организации/)
  assert.doesNotMatch(await render(true, 'admin'), /Удалить из организации/)
  assert.match(await render(true, 'viewer', true), /disabled=""[^>]*>Удалить из организации/)
})

test('removal dialog identifies the target, explains access revocation and preserves safe errors', async () => {
  const root = fileURLToPath(new URL('../../', import.meta.url))
  const result = await build({
    stdin: { contents: `
      import { createElement as h } from 'react'
      import { renderToStaticMarkup } from 'react-dom/server'
      import { RemoveMemberDialog } from './src/pages/organization/RemoveMemberDialog'
      export default renderToStaticMarkup(h(RemoveMemberDialog, {member:{email:'<img@example.com>'},pending:true,error:'Не удалось удалить участника',onClose(){},onConfirm(){}}))
    `, resolveDir: root },
    bundle:true, write:false, format:'esm', platform:'browser',
    alias:{'@':`${root}/src`}, define:{'process.env.NODE_ENV':'"production"'},
  })
  const html = (await import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`)).default
  assert.match(html, /role="dialog"/)
  assert.match(html, /&lt;img@example.com&gt;/)
  assert.match(html, /сеансы.*API-ключи/)
  assert.match(html, /История.*сохранятся/)
  assert.match(html, /role="alert"/)
  assert.match(html, /disabled=""[^>]*>Удаление…/)
})
