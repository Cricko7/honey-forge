import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'

test('live demo events preserve Geo/ASN while keeping captured data out of summaries', async () => {
  const result = await build({
    entryPoints: [fileURLToPath(new URL('./mock.ts', import.meta.url))],
    bundle: true, write: false, format: 'esm', platform: 'browser',
    alias: { '@': fileURLToPath(new URL('../', import.meta.url)) },
  })
  const { mockApi } = await import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`)
  const originalInterval = globalThis.setInterval
  let tick
  globalThis.setInterval = callback => { tick = callback; return 1 }
  let unsubscribe
  try {
    const notifications = []
    unsubscribe = mockApi.subscribe(event => notifications.push(event))
    tick()
    assert.equal(notifications.length, 1)
    const { event } = notifications[0]
    assert.match(event.source_enrichment.country_code, /^[A-Z]{2}$/)
    assert.ok(event.source_enrichment.asn > 0)
    assert.equal('data' in event, false)
  } finally {
    unsubscribe?.()
    globalThis.setInterval = originalInterval
  }
})
