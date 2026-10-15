import { useMemo, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { RefreshCw } from 'lucide-react'
import { useStatistics } from '@/api/use-statistics'
import { MAX_SAMPLE_SIZE, parseSampleSize } from '@/api/statistics'
import { useTraps } from '@/api/hooks'
import { summarizeStatistics } from '@/lib/statistics-summary'
import { severityLabel, type EventSeverity } from '@/lib/event-severity'
import { eventLabel } from '@/lib/ui-labels'
import { Spinner, EmptyState } from '@/components/ui'
import { GeoMap } from './statistics/GeoMap'
import { Breakdown } from './statistics/Breakdown'
import { Activity } from './statistics/Activity'

const fullDate = (at: number) => new Date(at).toLocaleString('ru-RU')
const percentage = (count: number, total: number) => `${Math.round(count / total * 100)}% выборки`

function Metric({ label, value, note }: { label: string; value: number | string; note?: string }) {
  return <div className="card p-4"><dt className="text-sm text-muted">{label}</dt><dd className="mt-2 text-2xl font-semibold tabular-nums">{value}</dd>{note && <p className="mt-1 text-xs text-muted">{note}</p>}</div>
}

export function StatisticsPage() {
  const [size, setSize] = useState(100)
  const [draft, setDraft] = useState('100')
  const [inputError, setInputError] = useState('')
  const query = useStatistics(size)
  const traps = useTraps()
  const trapNames = new Map(traps.data?.items.map(trap => [trap.id, trap.name]))
  const summary = useMemo(() => summarizeStatistics(query.data?.items ?? []), [query.data])

  function apply(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const parsed = parseSampleSize(draft)
    if (parsed === null) { setInputError(`Введите целое число от 1 до ${MAX_SAMPLE_SIZE}.`); return }
    setInputError('')
    if (parsed === size) void query.refetch()
    else setSize(parsed)
  }

  return <div className="page-content space-y-6">
    <header className="flex flex-wrap items-start justify-between gap-3">
      <div><h1 className="text-2xl font-semibold tracking-tight">Статистика</h1><p className="mt-1 text-sm text-muted">Источники и характер активности по последним событиям вашей организации.</p></div>
      <Link to="/app/events" className="btn-ghost">Открыть журнал</Link>
    </header>
    <section className="card p-4 sm:p-5" aria-label="Параметры выборки">
      <form className="flex flex-wrap items-end gap-3" onSubmit={apply}>
        <div><label htmlFor="sample-size" className="block text-sm font-medium mb-2">Последние N событий</label>
          <input id="sample-size" type="number" min="1" max={MAX_SAMPLE_SIZE} step="1" inputMode="numeric" required
            className="input w-36" value={draft} onChange={event => { setDraft(event.target.value); setInputError('') }}
            aria-invalid={!!inputError} aria-describedby={inputError ? 'sample-error sample-hint' : 'sample-hint'} />
        </div>
        <button type="submit" className="btn-primary" disabled={query.isFetching}>Рассчитать</button>
        <div className="flex flex-wrap gap-1" aria-label="Быстрый выбор размера">{[100, 250, 500, 1000].map(value => <button type="button" key={value}
          className={`btn-ghost tabular-nums ${size === value ? 'bg-brand/10 text-brand' : ''}`} aria-pressed={size === value} disabled={query.isFetching}
          onClick={() => { setDraft(String(value)); setSize(value); setInputError('') }}>{value}</button>)}</div>
        <button type="button" className="btn-ghost sm:ml-auto" onClick={() => { void query.refetch() }} disabled={query.isFetching}><RefreshCw size={15} aria-hidden="true" className={query.isFetching ? 'animate-spin' : ''} />Обновить</button>
      </form>
      {inputError && <p role="alert" id="sample-error" className="mt-2 text-sm text-danger">{inputError}</p>}
      <p id="sample-hint" className="mt-3 text-xs text-muted">От 1 до {MAX_SAMPLE_SIZE} событий, от новых к старым по времени возникновения. Все показатели рассчитаны по одной выборке; обновление — по кнопке.</p>
    </section>

    {query.error ? <div role="alert" className="card p-5 border-danger/30 flex flex-wrap items-center justify-between gap-3"><p className="text-sm">Не удалось загрузить полную выборку. Проверьте подключение и доступ к организации.</p><button type="button" className="btn-ghost" disabled={query.isFetching} onClick={() => { void query.refetch() }}>Повторить</button></div>
      : query.isPending || query.isFetching ? <div role="status" className="card p-8"><Spinner label="Загружаем выборку…" /></div>
      : summary.total === 0 ? <EmptyState title="Событий пока нет" hint="После подключения к ловушкам здесь появятся статистика и география источников." /> : <>
        <div role="status" className="text-sm text-muted space-y-1">
          <p><strong className="text-fg">Событий в расчёте: {summary.total} из {size} запрошенных.</strong> {summary.total < size && 'Доступных событий меньше N; использованы все доступные.'} {query.data?.hasMore && 'Более старые события не входят в расчёт.'}</p>
          <p>Период выборки: {summary.start !== null ? `${fullDate(summary.start)} — ${fullDate(summary.end!)}` : 'не определён'}. Время местное.</p>
          <p className="text-xs">Обновлено: {fullDate(query.dataUpdatedAt)}. Доли считаются от {summary.total} событий, уникальные IP и сессии — только внутри выборки.</p>
        </div>
        <dl className="grid grid-cols-2 lg:grid-cols-4 gap-3">
          <Metric label="События" value={summary.total} note="В выбранном срезе" />
          <Metric label="Уникальные IP" value={summary.uniqueSources} note="Источники событий" />
          <Metric label="Сессии" value={summary.sessions} note="Отдельно для каждой ловушки" />
          <Metric label="Ловушки с событиями" value={summary.traps.length} />
          <Metric label="Попытки входа" value={summary.authAttempts} note={percentage(summary.authAttempts, summary.total)} />
          <Metric label="Высокая и критическая важность" value={summary.highSeverity} note={percentage(summary.highSeverity, summary.total)} />
          <Metric label="Страны источников" value={summary.countries.length} note={`Без страны: ${summary.geoUnknown} событий`} />
          <Metric label="Покрытие геоданными" value={`${Math.round(summary.geoKnown / summary.total * 100)}%`} note={`${summary.geoKnown} из ${summary.total} событий`} />
        </dl>
        <GeoMap key={query.dataUpdatedAt} countries={summary.countries} unknown={summary.geoUnknown} total={summary.total} />
        <Activity buckets={summary.activity} />
        <div className="grid md:grid-cols-2 xl:grid-cols-3 gap-4">
          <Breakdown title="Типы событий" rows={summary.types} total={summary.total} label={eventLabel} />
          <Breakdown title="Важность" rows={summary.severities} total={summary.total} label={key => severityLabel(key as EventSeverity)} />
          <Breakdown title="Частые источники · IP" rows={summary.sources} total={summary.total} />
          <Breakdown title="Автономные системы · ASN" rows={summary.asns} total={summary.total} label={key => `AS${key}`} note={`ASN не определён: ${summary.asnUnknown} событий`} />
          <Breakdown title="Ловушки" rows={summary.traps} total={summary.total} label={key => trapNames.get(key) ?? `Ловушка ${key.slice(0, 8)}`} link={key => `/app/traps/${encodeURIComponent(key)}`} />
          <Breakdown title="Протоколы и порты назначения" rows={summary.services} total={summary.total} />
        </div>
      </>}
  </div>
}
