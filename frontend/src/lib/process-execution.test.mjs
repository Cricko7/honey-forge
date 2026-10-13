import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'

test('process telemetry shows metadata as escaped text and explains omitted arguments', async () => {
  const root = fileURLToPath(new URL('../../', import.meta.url))
  const result = await build({
    stdin: {
      contents: `
        import { createElement as h } from 'react'
        import { renderToStaticMarkup } from 'react-dom/server'
        import { ProcessExecutionDetails } from './src/components/ProcessExecutionDetails.tsx'
        export const html = renderToStaticMarkup(h(ProcessExecutionDetails, { data: {
          binary: '<script>alert(1)</script>', cwd: '/home/sandbox', pid: 2345, ppid: 1,
          uid: 1000, host_id: 'node-a', generation: 7, container_id: 'a'.repeat(64),
          container_started_at: '2026-10-13T12:00:00Z', sensor: 'tetragon', sensor_version: '1.7.0',
          arguments_redacted: true
        } }))
      `,
      resolveDir: root,
    },
    bundle: true, write: false, format: 'esm', platform: 'browser',
    define: { 'process.env.NODE_ENV': '"production"' },
  })
  const { html } = await import(`data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`)
  for (const value of ['2345', '1000', 'node-a', 'tetragon', '1.7.0', 'Аргументы не собираются', '&lt;script&gt;']) {
    assert.ok(html.includes(value), `missing ${value}`)
  }
  assert.ok(!html.includes('<script>'))
})
