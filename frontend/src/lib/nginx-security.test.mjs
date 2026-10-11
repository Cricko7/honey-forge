import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

for (const [name, path, host] of [
  ['local dashboard', '../../nginx.conf', 'localhost'],
  ['deployment management', '../../../deploy/nginx/honey-forge.conf', 'honey-forge-seg-fault.ru'],
]) {
  test(`${name} prevents an attacker from framing management pages`, () => {
    const config = readFileSync(new URL(path, import.meta.url), 'utf8')
    const server = config.match(/server \{[\s\S]*?\r?\n\}/g)
      .find((block) => block.includes(`server_name ${host};`))
    assert.ok(server, 'management virtual host must exist')
    assert.match(server, /add_header X-Frame-Options DENY always;/)
    assert.match(server, /add_header Content-Security-Policy "frame-ancestors 'none'" always;/)
  })
}
