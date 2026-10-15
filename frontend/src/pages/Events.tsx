import { eventSeverity } from '@/lib/event-severity'
import { SourceGeo } from '@/components/SourceGeo'
import { Select } from '@/components/Select'
import { DateTimePicker } from '@/components/DateTimePicker'
import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { EventTypeBadge, Spinner, EmptyState, LoadError } from '@/components/ui'
import { EventDetailModal } from '@/components/EventDetailModal'
import { useCatalog, useEvents, useTraps } from '@/api/hooks'
import { api } from '@/api/client'
import { journalFilter } from '@/api/event-filters'
import { formatDateTime } from '@/lib/utils'
import { useAlerts } from '@/components/Alerts'
import { actionErrorMessage } from '@/lib/alerts'
import { eventLabel } from '@/lib/ui-labels'

export function EventsPage() {
  const { notify } = useAlerts()
  const [searchParams, setSearchParams] = useSearchParams()
  const empty = { trap_id: '', type_id: '', event_type: '', from: '', to: '' }
  const [filters, setFilters] = useState(empty)
  const [cursors, setCursors] = useState<string[]>([])
  const [exporting, setExporting] = useState(false)
  const [downloadError, setDownloadError] = useState('')
  const { data: traps } = useTraps()
  const { data: catalog } = useCatalog()
  let filter = {}
  let validationError = ''
  try { filter = journalFilter(filters) } catch (e) { validationError = (e as Error).message }
  const { data, isLoading, error, refetch } = useEvents({ ...filter, limit: 100, cursor: cursors.at(-1) }, !validationError)
  const selected = searchParams.get('event')
  const setSelected = (id: string | null) => {
    setSearchParams((previous) => {
      const next = new URLSearchParams(previous)
      if (id) next.set('event', id)
      else next.delete('event')
      return next
    })
  }
  const events = data?.items ?? []
  const types = [...new Set((catalog?.items ?? []).map((entry) => entry.type_id))].sort()
  const eventTypes = [...new Set((catalog?.items ?? []).flatMap((entry) => entry.event_schemas.map((event) => event.event_type)))].sort()
  const change = (key: keyof typeof filters, value: string) => {
    setFilters((previous) => ({ ...previous, [key]: value }))
    setCursors([])
    setDownloadError('')
  }
  const download = async (format: 'csv' | 'stix') => {
    setExporting(true)
    setDownloadError('')
    try {
      const blob = await api.exportEvents(format, filter)
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = `events-${new Date().toISOString().replaceAll(':', '-')}.${format === 'csv' ? 'csv' : 'stix.json'}`
      document.body.appendChild(link)
      link.click()
      link.remove()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
      notify({ id: crypto.randomUUID(), title: 'Экспорт событий готов', message: `Файл ${format.toUpperCase()} подготовлен.`, level: 'success' })
    } catch (e) {
      setDownloadError(e instanceof Error ? e.message : 'Не удалось скачать файл')
      notify({ id: crypto.randomUUID(), title: 'Не удалось экспортировать события', message: actionErrorMessage(e), level: 'error' })
    }
    finally { setExporting(false) }
  }

  return (
    <div className="page-content space-y-5">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-2xl font-semibold tracking-tight">События</h1>
        <div role="group" aria-label="Экспорт событий" className="flex items-center gap-2">
          <button className="btn-ghost" disabled={exporting || !!validationError} onClick={() => download('csv')}>Скачать CSV</button>
          <button className="btn-ghost" disabled={exporting || !!validationError} onClick={() => download('stix')}>Скачать STIX</button>
        </div>
      </header>

      <section aria-label="Фильтры событий" className="border-y border-line py-4">
        <div className="grid grid-cols-1 min-[360px]:grid-cols-2 xl:grid-cols-5 gap-3">
          <label className="text-xs text-muted">Тип ловушки
            <Select label="Тип ловушки" className="mt-1" value={filters.type_id} onChange={(value) => change('type_id', value)}
              options={[{ value: '', label: 'Все типы' }, ...types.map((type) => ({ value: type, label: type }))]} />
          </label>
          <label className="text-xs text-muted">Ловушка
            <Select label="Ловушка" className="mt-1" value={filters.trap_id} onChange={(value) => change('trap_id', value)}
              options={[{ value: '', label: 'Все ловушки' }, ...(traps?.items ?? []).map((trap) => ({ value: trap.id, label: trap.name }))]} />
          </label>
          <label className="text-xs text-muted min-[360px]:col-span-2 xl:col-span-1">Тип события
            <Select label="Тип события" className="mt-1" value={filters.event_type} onChange={(value) => change('event_type', value)}
              options={[{ value: '', label: 'Все события' }, ...eventTypes.map((type) => ({ value: type, label: eventLabel(type) }))]} />
          </label>
          <label className="text-xs text-muted">С (включительно)
            <DateTimePicker label="С (включительно)" className="mt-1" value={filters.from} onChange={(value) => change('from', value)} />
          </label>
          <label className="text-xs text-muted">До (не включительно)
            <DateTimePicker label="До (не включительно)" className="mt-1" value={filters.to} onChange={(value) => change('to', value)} />
          </label>
        </div>
        <div className="mt-3 flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
          <p role="status" className="text-xs text-muted">{exporting ? 'Подготовка файла…' : 'Даты — местные. Экспорт — по всем найденным событиям.'}</p>
          <button className="min-h-11 text-sm text-brand hover:underline disabled:text-muted disabled:no-underline disabled:cursor-not-allowed" disabled={!Object.values(filters).some(Boolean)} onClick={() => { setFilters(empty); setCursors([]); setDownloadError('') }}>Сбросить фильтры</button>
        </div>
      </section>
      {(validationError || downloadError) && <p role="alert" className="text-sm text-danger">{validationError || downloadError}</p>}
      {error && <LoadError message="Не удалось загрузить события. Проверьте подключение." onRetry={() => { void refetch() }} />}

      <p className="text-xs text-muted">Важность: <span className="event-label-low">обычное</span> · <span className="event-label-medium">средняя — жёлтый</span> · <span className="event-label-high">высокая — красный</span> · <span className="event-label-critical">критическая — багровый</span></p>
      <div className="overflow-x-auto">
        {validationError || error ? null : isLoading ? (
          <Spinner />
        ) : events.length === 0 ? (
          <EmptyState title="Событий нет" />
        ) : (
          <table aria-label="Журнал событий" className="data-table w-full min-w-[800px] text-sm">
            <thead>
              <tr className="text-left text-xs text-muted border-b border-line/60">
                <th scope="col" className="px-3 py-3 font-medium">Время</th>
                <th scope="col" className="px-3 py-3 font-medium">Тип события</th>
                <th scope="col" className="px-3 py-3 font-medium">Источник</th>
                <th scope="col" className="px-3 py-3 font-medium">Назначение</th>
                <th scope="col" className="px-3 py-3 font-medium">Тип ловушки</th>
              </tr>
            </thead>
            <tbody>
              {events.map((e) => (
                <tr
                  key={e.event_id}
                  onClick={() => setSelected(e.event_id)}
                  data-severity={eventSeverity(e)}
                  className={`event-surface event-surface-${eventSeverity(e)} border-b border-line/40 last:border-0 cursor-pointer transition-colors`}
                >
                  <td className="px-3 py-3 tabular-nums text-xs text-muted whitespace-nowrap">
                    <button type="button" className="text-left hover:text-brand focus-visible:underline"
                      aria-label={`Открыть событие ${e.event_type}, ${formatDateTime(e.occurred_at)}`}>
                      {formatDateTime(e.occurred_at)}
                    </button>
                  </td>
                  <td className="px-3 py-3">
                    <EventTypeBadge type={e.event_type} severity={eventSeverity(e)} />
                  </td>
                  <td className="px-3 py-3 font-mono text-xs">
                    {e.source.ip}:{e.source.port}
                    <SourceGeo enrichment={e.source_enrichment} />
                  </td>
                  <td className="px-3 py-3 font-mono text-xs text-muted">
                    {e.destination.protocol}:{e.destination.port}
                  </td>
                  <td className="px-3 py-3 font-mono text-xs text-muted">
                    {e.type_id}/{e.type_version}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
      <div className="flex items-center gap-3">
        <button className="btn-ghost" disabled={cursors.length === 0 || isLoading} onClick={() => setCursors((previous) => previous.slice(0, -1))}>Назад</button>
        <span className="text-xs text-muted">Страница {cursors.length + 1} · {events.length} событий</span>
        <button className="btn-ghost" disabled={!data?.next_cursor || isLoading || !!validationError || !!error} onClick={() => { if (data?.next_cursor) setCursors((previous) => [...previous, data.next_cursor!]) }}>Далее</button>
      </div>

      <EventDetailModal id={selected} onClose={() => setSelected(null)} />
    </div>
  )
}
