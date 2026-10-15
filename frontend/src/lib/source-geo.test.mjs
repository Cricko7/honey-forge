import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'

const root = fileURLToPath(new URL('../../', import.meta.url))
const result = await build({
  stdin: { contents: `
    import { createElement as h } from 'react'
    import { renderToStaticMarkup } from 'react-dom/server'
    import { SourceGeo } from './src/components/SourceGeo'
    export const render = (enrichment) => renderToStaticMarkup(h(SourceGeo, { enrichment }))
  `, resolveDir: root },
  bundle: true, write: false, format: 'esm', platform: 'browser',
  alias: { '@': `${root}/src` },
  define: { 'process.env.NODE_ENV': '"production"' },
})
const { render } = await import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`)

test('source geo shows the country, ASN and data attribution', () => {
  const html = render({ country_code: 'US', asn: 15169 })
  assert.match(html, /Соединенные Штаты|Соединённые Штаты/)
  assert.match(html, /AS15169/)
  assert.match(html, /href="https:\/\/db-ip.com"/)
})

test('unknown and older events have explicit empty states without a fabricated ASN', () => {
  for (const enrichment of [undefined, null, { country_code: null, asn: null }]) {
    const html = render(enrichment)
    assert.match(html, /Страна не определена/)
    assert.match(html, /ASN не определён/)
    assert.doesNotMatch(html, /AS—|ASnull|ASundefined/)
  }
  assert.match(render({ country_code: null, asn: 15169 }), /AS15169/)
  assert.match(render({ country_code: 'DE', asn: null }), /Германия/)
})

test('untrusted country strings never become markup', () => {
  assert.doesNotMatch(render({ country_code: '<img src=x onerror=alert(1)>', asn: null }), /<img/)
})
