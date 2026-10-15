import { useState } from 'react'
import world from '@/lib/world-map.json'
import { countryName, type CountryCount } from '@/lib/statistics-summary'

export function GeoMap({ countries, unknown, total }: { countries: CountryCount[]; unknown: number; total: number }) {
  const [selected, setSelected] = useState<string | null>(null)
  const [expanded, setExpanded] = useState(false)
  const counts = new Map(countries.map(country => [country.key, country]))
  const maximum = countries[0]?.count ?? 1
  const active = counts.get(selected ?? '')
  const mapped = new Set(world.map(country => country.code))
  const coverage = countries.reduce((sum, country) => sum + (mapped.has(country.key) ? country.count : 0), 0)

  return <section className="card p-4 sm:p-6" aria-labelledby="geography-title">
    <div className="flex flex-wrap items-baseline justify-between gap-2 mb-4">
      <h2 id="geography-title" className="text-base font-semibold">География источников</h2>
      <span className="text-xs text-muted">Страна определена: {total - unknown} из {total}</span>
    </div>
    <div className="grid gap-6 xl:grid-cols-[minmax(0,1.7fr)_minmax(15rem,1fr)]">
      <div className="min-w-0">
        <div className="rounded-xl border border-line bg-bg-elevated overflow-hidden">
          <svg viewBox="0 0 960 400" className="w-full h-auto" aria-label="Карта стран источников" role="group">
            <title>Страны источников событий. Темнее — больше событий.</title>
            <g stroke="rgb(var(--line))" strokeWidth="0.7" strokeLinejoin="round">
              {world.map(country => {
                const row = counts.get(country.code)
                const label = row ? `${countryName(country.code)}: событий — ${row.count}, IP — ${row.sources}` : undefined
                return <path key={country.code} d={country.path}
                  fill={row ? 'rgb(var(--brand))' : 'rgb(var(--bg-panel))'}
                  fillOpacity={row ? 0.3 + 0.7 * Math.sqrt(row.count / maximum) : 1}
                  stroke={selected === country.code ? 'rgb(var(--fg))' : undefined}
                  strokeWidth={selected === country.code ? 2 : undefined}
                  role={row ? 'button' : undefined} tabIndex={row ? 0 : undefined}
                  aria-label={label} aria-pressed={row ? selected === country.code : undefined}
                  className={row ? 'cursor-pointer hover:opacity-80 focus:outline-none focus:stroke-fg focus:stroke-2' : undefined}
                  onClick={row ? () => setSelected(country.code) : undefined}
                  onKeyDown={row ? event => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); setSelected(country.code) } } : undefined}>
                  {label && <title>{label}</title>}
                </path>
              })}
            </g>
          </svg>
        </div>
        <div className="mt-3 flex items-center gap-2 text-xs text-muted"><span>Меньше</span><span className="h-2 w-24 rounded bg-gradient-to-r from-brand/30 to-brand" /><span>Больше событий</span></div>
        <p className="mt-3 text-sm min-h-10" aria-live="polite">{active ? <><strong>{countryName(active.key)}</strong> · События: {active.count} · Уникальные IP: {active.sources}</> : 'Выберите страну на карте или в списке, чтобы увидеть число событий и IP.'}</p>
        <p className="mt-2 text-xs text-muted">Страна IP по DB-IP, без точной геолокации. Без страны: {unknown}. {coverage < total - unknown && 'Некоторые малые территории доступны только в списке.'}</p>
        <p className="mt-1 text-xs text-muted">Геоданные: <a className="underline" href="https://db-ip.com" target="_blank" rel="noopener noreferrer">DB-IP</a>. Основа: <a className="underline" href="https://www.naturalearthdata.com/about/terms-of-use/" target="_blank" rel="noopener noreferrer">Natural Earth</a>.</p>
      </div>
      <div>
        <div className="flex justify-between text-xs text-muted border-b border-line pb-2"><span>Страна</span><span>События · доля выборки</span></div>
        {countries.length ? <ul className="divide-y divide-line/60">{(expanded ? countries : countries.slice(0, 8)).map(row => <li key={row.key}>
          <button type="button" onClick={() => setSelected(row.key)} aria-pressed={selected === row.key}
            className={`flex min-h-11 w-full items-center justify-between gap-3 rounded px-2 py-2 text-left text-sm hover:bg-bg-hover ${selected === row.key ? 'bg-brand/10 text-brand' : ''}`}>
            <span>{countryName(row.key)}</span><span className="shrink-0 tabular-nums">{row.count} <span className="text-muted">· {Math.round(row.count / total * 100)}%</span></span>
          </button>
        </li>)}</ul> : <p className="py-5 text-sm text-muted">Страны источников пока не определены.</p>}
        {countries.length > 8 && <button type="button" className="btn-ghost mt-2" aria-expanded={expanded} onClick={() => setExpanded(!expanded)}>{expanded ? 'Свернуть список' : `Все страны (${countries.length})`}</button>}
      </div>
    </div>
  </section>
}
