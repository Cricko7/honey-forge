// Source: Natural Earth v5.1.2, ne_110m_admin_0_countries.geojson (public domain).
// Download the source explicitly, then run: node scripts/generate-statistics-map.mjs <file.geojson>
// No remote assets or source metadata are loaded by the application at runtime.
import { readFileSync, writeFileSync } from 'node:fs'

const source = JSON.parse(readFileSync(process.argv[2], 'utf8'))
const countries = source.features.filter(f => f.properties.ISO_A2_EH !== 'AQ').map(feature => {
  // Territories without an ISO code remain uncolored background geometry.
  const code = feature.properties.ISO_A2_EH === '-99' ? feature.properties.ADM0_A3 : feature.properties.ISO_A2_EH
  if (!/^[A-Z]{2,3}$/.test(code)) throw new Error('Country has no code')
  const polygons = feature.geometry.type === 'Polygon' ? [feature.geometry.coordinates] : feature.geometry.coordinates
  const path = polygons.flatMap(polygon => polygon.map(ring => ring.map(([lon, lat], i) => {
    if (!Number.isFinite(lon) || !Number.isFinite(lat) || Math.abs(lon) > 180 || Math.abs(lat) > 90) throw new Error('Invalid coordinate')
    return `${i ? 'L' : 'M'}${((lon + 180) * 8 / 3).toFixed(1)},${((85 - lat) * 8 / 3).toFixed(1)}`
  }).join('') + 'Z')).join('')
  return { code, path }
})
writeFileSync(new URL('../frontend/src/lib/world-map.json', import.meta.url), JSON.stringify(countries) + '\n')
