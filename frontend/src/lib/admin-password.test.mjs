import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'

test('admin password form starts empty and requests the current account password', async () => {
  const root = fileURLToPath(new URL('../../', import.meta.url))
  const result = await build({
    stdin: { contents: `
      import { createElement as h } from 'react'
      import { renderToStaticMarkup } from 'react-dom/server'
      import { AdminPasswordForm } from './src/pages/organization/AdminPasswordForm'
      export default renderToStaticMarkup(h(AdminPasswordForm, {onConfirm:async()=>{}}))
    `, resolveDir:root },
    bundle:true, write:false, format:'esm', platform:'browser',
    alias:{'@':`${root}/src`}, define:{'process.env.NODE_ENV':'"production"'},
  })
  const html = (await import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`)).default
  assert.match(html, /type="password"/)
  assert.match(html, /autoComplete="current-password"/)
  assert.match(html, /value=""/)
  assert.match(html, /Пароль аккаунта/)
  assert.doesNotMatch(html, /demo-password|localStorage|sessionStorage/)
})

for (const [name, expires, revealed] of [
  ['locked', null, false],
  ['expired', '2000-01-01T00:00:00Z', false],
  ['confirmed', '2099-01-01T00:00:00Z', true],
]) {
  test(`admin gate ${name} state controls whether private content mounts`, async () => {
    const root = fileURLToPath(new URL('../../', import.meta.url))
    const result = await build({
      stdin: {contents:`
        import {createElement as h} from 'react'
        import {renderToStaticMarkup} from 'react-dom/server'
        import {QueryClient, QueryClientProvider} from '@tanstack/react-query'
        import {AdminAccessGate} from './src/pages/organization/AdminAccessGate'
        const client = new QueryClient()
        client.setQueryData(['admin-access','org','user'], {expires_at:${JSON.stringify(expires)}})
        let mounted = false
        function Secret() { mounted = true; return h('p',{},'private-team-data') }
        const html = renderToStaticMarkup(h(QueryClientProvider,{client},h(AdminAccessGate,{session:{user:{id:'user'},organization:{id:'org'}}},h(Secret))))
        export default {html,mounted}
      `,resolveDir:root},
      bundle:true,write:false,format:'esm',platform:'browser',alias:{'@':`${root}/src`},
      define:{'process.env.NODE_ENV':'"production"','import.meta.env.VITE_API_BASE':'"https://operator.example"'},
    })
    const output = (await import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`)).default
    assert.equal(output.mounted, revealed)
    assert.equal(output.html.includes('private-team-data'), revealed)
  })
}
