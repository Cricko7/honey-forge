import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

test('bundled map contains only unique codes and bounded numeric SVG contours', () => {
  const countries = JSON.parse(readFileSync(new URL('./world-map.json', import.meta.url), 'utf8'))
  const codes = new Set()
  for (const country of countries) {
    assert.deepEqual(Object.keys(country).sort(), ['code', 'path'])
    assert.match(country.code, /^[A-Z]{2,3}$/)
    assert.ok(!codes.has(country.code))
    codes.add(country.code)
    assert.match(country.path, /^M[MLZ\d.,-]+Z$/)
    for (const match of country.path.matchAll(/[ML](-?[\d.]+),(-?[\d.]+)/g)) {
      const [x, y] = match.slice(1).map(Number)
      assert.ok(x >= 0 && x <= 960 && y >= 0 && y <= 400, country.code)
    }
  }
  for (const code of ['RU', 'DE', 'US', 'CN', 'IN', 'BR', 'AU']) assert.ok(codes.has(code), code)
})
