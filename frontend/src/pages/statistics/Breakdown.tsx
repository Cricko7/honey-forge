import { useState } from 'react'
import { Link } from 'react-router-dom'
import type { StatisticCount } from '@/lib/statistics-summary'

export function Breakdown({ title, rows, total, label = key => key, link, note }: {
  title: string; rows: StatisticCount[]; total: number; label?: (key: string) => string
  link?: (key: string) => string; note?: string
}) {
  const [expanded, setExpanded] = useState(false)
  const visible = expanded ? rows : rows.slice(0, 6)
  return <section className="card p-4 sm:p-5 min-w-0" aria-label={title}>
    <h2 className="font-semibold">{title}</h2>
    {note && <p className="mt-1 text-xs text-muted">{note}</p>}
    {rows.length ? <ol className={`mt-4 space-y-4 ${expanded ? 'max-h-96 overflow-y-auto pr-2' : ''}`}>{visible.map(row => <li key={row.key}>
      <div className="flex items-baseline justify-between gap-3 text-sm">
        {link ? <Link className="min-w-0 break-all text-brand hover:underline" title={row.key} to={link(row.key)}>{label(row.key)}</Link> : <span className="min-w-0 break-all" title={row.key}>{label(row.key)}</span>}
        <span className="shrink-0 tabular-nums">{row.count} <span className="text-muted text-xs">· {Math.round(row.count / total * 100)}%</span></span>
      </div>
      <div className="mt-1.5 h-1 rounded bg-bg-elevated" aria-hidden="true"><div className="h-full rounded bg-brand/60" style={{ width: `${row.count / total * 100}%` }} /></div>
    </li>)}</ol> : <p className="mt-4 text-sm text-muted">Нет определённых данных.</p>}
    {rows.length > 6 && <button className="btn-ghost mt-4" type="button" aria-expanded={expanded} onClick={() => setExpanded(!expanded)}>{expanded ? 'Свернуть' : `Показать все (${rows.length})`}</button>}
  </section>
}
