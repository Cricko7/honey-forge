import { eventSeverity } from '@/lib/event-severity'
import { SourceGeo } from '@/components/SourceGeo'
import { Link } from 'react-router-dom'
import { EventTypeBadge, Spinner, EmptyState } from '@/components/ui'
import { useCanManage, useEvents, useTraps } from '@/api/hooks'
import { formatDateTime } from '@/lib/utils'

function Metric({ label, value, note }: { label: string; value: number | undefined; note?: string }) {
  return <div className="py-3">
    <dt className="text-sm text-muted">{label}</dt>
    <dd className="mt-1 flex flex-wrap items-baseline gap-x-2 gap-y-1 text-2xl font-semibold tabular-nums tracking-tight">{value ?? '—'}
      {note && <span className="text-xs font-normal tracking-normal text-muted">{note}</span>}
    </dd>
  </div>
}

export function DashboardPage() {
  const canManage = useCanManage()
  const trapsQuery = useTraps()
  const eventsQuery = useEvents({ limit: 8 })
  const traps = trapsQuery.data?.items ?? []
  const events = eventsQuery.data?.items ?? []
  const haveTraps = !!trapsQuery.data
  const haveEvents = !!eventsQuery.data
  const failed = trapsQuery.error || eventsQuery.error

  return <div className="page-content space-y-7">
    <div className="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Обзор</h1>
        <p className="mt-1 text-sm text-muted">Состояние ловушек и последние события.</p>
      </div>
      {canManage && <Link to="/app/network?create=1" className="btn-primary">Создать ловушку</Link>}
    </div>

    {failed && <div role="alert" className="flex flex-wrap items-center justify-between gap-3 border border-danger/30 rounded-lg px-4 py-3 text-sm">
      <p>Не удалось обновить {trapsQuery.error && eventsQuery.error ? 'ловушки и события' : trapsQuery.error ? 'ловушки' : 'события'}. {haveTraps || haveEvents ? 'Показаны доступные данные.' : 'Проверьте подключение.'}</p>
      <button type="button" className="btn-ghost" onClick={() => { void trapsQuery.refetch(); void eventsQuery.refetch() }}>Повторить</button>
    </div>}

    <dl className="grid grid-cols-2 gap-x-6 gap-y-2 border-y border-line py-2">
      <Metric label="На связи" value={haveTraps ? traps.filter(t => t.connectivity === 'online').length : undefined} note={haveTraps ? `из ${traps.length}${trapsQuery.data?.next_cursor ? '+ ловушек' : ' ловушек'}` : 'Данные о ловушках'} />
      <Metric label="Ловушки без связи" value={haveTraps ? traps.filter(t => t.connectivity !== 'online').length : undefined} note="В загруженном списке" />
    </dl>

    <div className="card p-4 flex flex-wrap items-center justify-between gap-3">
      <div><h2 className="text-base font-semibold">Статистика событий</h2><p className="mt-1 text-sm text-muted">Карта стран, активность и источники по последним N событиям.</p></div>
      <Link to="/app/statistics" className="btn-ghost text-brand">Открыть статистику</Link>
    </div>

    <section aria-labelledby="recent-events-title" className="min-w-0 border-t border-line pt-6">
      <div className="mb-4 flex items-center justify-between gap-3">
        <h2 id="recent-events-title" className="text-base font-semibold">Последние события</h2>
        <Link to="/app/events" className="text-sm text-brand hover:underline">Открыть журнал</Link>
      </div>
      {eventsQuery.isLoading ? <Spinner /> : !haveEvents ? <p className="px-5 pb-5 text-sm text-muted">Данные недоступны.</p> : events.length === 0 ? <EmptyState title="Событий пока нет" hint="После подключения к ловушке события появятся здесь и в журнале." /> : <div className="overflow-x-auto">
        <table className="data-table w-full text-sm text-left">
          <thead className="border-y border-line text-xs text-muted"><tr>
            <th scope="col" className="px-5 py-2.5 font-medium">Время</th>
            <th scope="col" className="px-5 py-2.5 font-medium">Источник</th>
            <th scope="col" className="px-5 py-2.5 font-medium">Событие</th>
            <th scope="col" className="px-5 py-2.5 font-medium text-right">Подробности</th>
          </tr></thead>
          <tbody className="divide-y divide-line/60">{events.slice(0, 8).map(event => <tr key={event.event_id} className="hover:bg-bg-hover/50">
            <td className="px-5 py-3 whitespace-nowrap tabular-nums text-muted">{formatDateTime(event.occurred_at)}</td>
            <td className="px-5 py-3 font-mono text-xs">{event.source.ip}<SourceGeo enrichment={event.source_enrichment} /></td>
            <td className="px-5 py-3"><EventTypeBadge type={event.event_type} severity={eventSeverity(event)} /></td>
            <td className="px-5 py-3 text-right"><Link to={`/app/events?event=${encodeURIComponent(event.event_id)}`} className="text-brand hover:underline" aria-label={`Открыть событие ${event.event_type} от ${event.source.ip}`}>Открыть</Link></td>
          </tr>)}</tbody>
        </table>
      </div>}
    </section>

  </div>
}
