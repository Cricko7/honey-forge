import test from 'node:test'
import assert from 'node:assert/strict'
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'
import { conpotProtocols } from './service-protocols.ts'

test('Conpot exposes five protocol choices and quick creation preserves the selection', async () => {
  const root = fileURLToPath(new URL('../../', import.meta.url))
  const built = await build({
    stdin: { contents: `
      import { createElement as h } from 'react'
      import { renderToStaticMarkup } from 'react-dom/server'
      import { ProfileConfigEditor } from './src/pages/profiles/ProfileConfigEditor'
      import { defaultConfig } from './src/pages/network/AddTrapModal'
      export { defaultConfig }
      export const render = (config) => renderToStaticMarkup(h(ProfileConfigEditor, { typeId: 'conpot', value: JSON.stringify(config), onChange() {} }))
    `, resolveDir: root },
    bundle: true, write: false, format: 'esm', platform: 'browser', alias: { '@': `${root}/src` },
    define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env.VITE_API_BASE': '""', 'import.meta.env.VITE_USE_MOCK': '"false"' },
  })
  const { defaultConfig, render } = await import(`data:text/javascript;base64,${Buffer.from(built.outputFiles[0].text).toString('base64')}`)
  for (const protocol of conpotProtocols) {
    const config = defaultConfig('conpot', protocol.port, protocol.id)
    assert.equal(config.services[0].protocol, protocol.id)
    assert.equal(config.services[0].port, protocol.port)
    const html = render(config)
    assert.match(html, /Промышленный протокол/)
    assert.ok(html.includes(`<option value="${protocol.id}" selected="">`))
    for (const option of conpotProtocols) assert.ok(html.includes(option.label))
    assert.ok(!Object.hasOwn(config, 'listeners'))
  }
})
