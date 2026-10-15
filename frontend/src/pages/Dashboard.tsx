import { eventSeverity } from '@/lib/event-severity'
import { Link } from 'react-router-dom'
import { ResponsiveContainer, LineChart, Line, XAxis, YAxis, Tooltip, CartesianGrid } from 'recharts'
import { SectionTitle, EventTypeBadge, Spinner, EmptyState } from '@/components/ui'
import { useCanManage, useEvents, useTraps } from '@/api/hooks'
import { formatDateTime } from '@/lib/utils'
import { useThemeColors } from '@/lib/theme'
import { summarizeDashboard } from '@/lib/dashboard-summary'
import { eventLabel } from '@/lib/ui-labels'

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
  const c = useThemeColors()
  const trapsQuery = useTraps()
  const eventsQuery = useEvents({ limit: 100 })
  const traps = trapsQuery.data?.items ?? []
  const events = eventsQuery.data?.items ?? []
  const summary = summarizeDashboard(events)
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

    <dl className="grid grid-cols-2 xl:grid-cols-4 gap-x-6 gap-y-2 border-y border-line py-2">
      <Metric label="На связи" value={haveTraps ? traps.filter(t => t.connectivity === 'online').length : undefined} note={haveTraps ? `из ${traps.length}${trapsQuery.data?.next_cursor ? '+ ловушек' : ' ловушек'}` : 'Данные о ловушках'} />
      <Metric label="За 24 часа в выборке" value={haveEvents ? summary.events24h : undefined} />
      <Metric label="Попытки входа" value={haveEvents ? summary.authAttempts : undefined} />
      <Metric label="Источники · IP" value={haveEvents ? summary.uniqueSources : undefined} />
    </dl>

    <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted">
      <span>Выборка: последние 100 событий. По ней рассчитаны активность и источники.</span>
    </div>

    <section aria-labelledby="activity-title" className="min-w-0">
      <h2 id="activity-title" className="mb-5 text-base font-semibold">Активность за 24 часа</h2>
      {eventsQuery.isLoading ? <Spinner /> : !haveEvents ? <p className="text-sm text-muted">Данные недоступны.</p> : summary.events24h === 0 ? <EmptyState title="За последние 24 часа событий в выборке нет" /> : <>
        <div className="h-56" role="img" aria-label={`События по часам за последние 24 часа: ${summary.events24h}`}>
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={summary.activity} margin={{ left: -20, right: 12 }}>
              <CartesianGrid stroke={c.line} vertical={false} />
              <XAxis dataKey="at" stroke={c.muted} fontSize={11} tickLine={false} interval={3}
                tickFormatter={at => new Date(at).toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })} />
              <YAxis stroke={c.muted} fontSize={11} tickLine={false} axisLine={false} allowDecimals={false} />
              <Tooltip labelFormatter={at => formatDateTime(new Date(Number(at)).toISOString())} contentStyle={{ background: c.panel, border: `1px solid ${c.line}`, borderRadius: 6, fontSize: 12 }} />
              <Line name="События" type="linear" dataKey="count" stroke={c.brand} strokeWidth={2} dot={false} isAnimationActive={false} />
            </LineChart>
          </ResponsiveContainer>
        </div>
        <p className="mt-2 text-xs text-muted">Время местное. Первый и последний час могут быть неполными.</p>
      </>}
    </section>

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
            <td className="px-5 py-3 font-mono text-xs">{event.source.ip}</td>
            <td className="px-5 py-3"><EventTypeBadge type={event.event_type} severity={eventSeverity(event)} /></td>
            <td className="px-5 py-3 text-right"><Link to={`/app/events?event=${encodeURIComponent(event.event_id)}`} className="text-brand hover:underline" aria-label={`Открыть событие ${event.event_type} от ${event.source.ip}`}>Открыть</Link></td>
          </tr>)}</tbody>
        </table>
      </div>}
    </section>

    <div className="grid grid-cols-1 lg:grid-cols-2 gap-8 border-t border-line pt-6">
      <section className="min-w-0">
        <SectionTitle>Типы событий</SectionTitle>
        {haveEvents ? summary.byType.length ? <ul className="divide-y divide-line/60">{summary.byType.map(item => <li key={item.type} className="flex items-center justify-between gap-4 py-2 text-sm">
          <span title={item.type} className="text-sm break-words">{eventLabel(item.type)}</span><span className="tabular-nums text-muted">{item.count}</span>
        </li>)}</ul> : <p className="text-sm text-muted">Нет данных для сравнения.</p> : <p className="text-sm text-muted">{eventsQuery.isLoading ? 'Загрузка…' : 'Данные недоступны.'}</p>}
      </section>
      <section className="min-w-0">
        <SectionTitle>Частые источники</SectionTitle>
        {haveEvents ? summary.sources.length ? <ol className="divide-y divide-line/60">{summary.sources.map(item => <li key={item.ip} className="flex items-center justify-between gap-4 py-2 text-sm">
          <span className="font-mono text-xs break-all">{item.ip}</span><span className="tabular-nums text-muted">{item.count}</span>
        </li>)}</ol> : <p className="text-sm text-muted">Нет данных для сравнения.</p> : <p className="text-sm text-muted">{eventsQuery.isLoading ? 'Загрузка…' : 'Данные недоступны.'}</p>}
      </section>
    </div>
  </div>
}
