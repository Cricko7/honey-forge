import { Select } from '@/components/Select'
import { DateTimePicker } from '@/components/DateTimePicker'
import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Card, EventTypeBadge, Spinner, EmptyState } from '@/components/ui'
import { EventDetailModal } from '@/components/EventDetailModal'
import { useCatalog, useEvents, useTraps } from '@/api/hooks'
import { api } from '@/api/client'
import { journalFilter } from '@/api/event-filters'
import { formatDateTime } from '@/lib/utils'
import { useAlerts } from '@/components/Alerts'
import { actionErrorMessage } from '@/lib/alerts'

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
  const { data, isLoading, error } = useEvents({ ...filter, limit: 100, cursor: cursors.at(-1) }, !validationError)
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
    <div className="p-6 space-y-5">
      <div>
        <h1 className="text-xl font-semibold">События</h1>
        <p className="text-sm text-muted">
          Журнал активности атакующих · перехваченные подключения, креды и команды
        </p>
      </div>

      <Card>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
          <label className="text-xs text-muted">Тип ловушки
            <Select label="Тип ловушки" className="mt-1" value={filters.type_id} onChange={(value) => change('type_id', value)}
              options={[{ value: '', label: 'Все типы' }, ...types.map((type) => ({ value: type, label: type }))]} />
          </label>
          <label className="text-xs text-muted">Ловушка
            <Select label="Ловушка" className="mt-1" value={filters.trap_id} onChange={(value) => change('trap_id', value)}
              options={[{ value: '', label: 'Все ловушки' }, ...(traps?.items ?? []).map((trap) => ({ value: trap.id, label: trap.name }))]} />
          </label>
          <label className="text-xs text-muted">Тип события
            <Select label="Тип события" className="mt-1" value={filters.event_type} onChange={(value) => change('event_type', value)}
              options={[{ value: '', label: 'Все события' }, ...eventTypes.map((type) => ({ value: type, label: type }))]} />
          </label>
          <label className="text-xs text-muted">С (включительно)
            <DateTimePicker label="С (включительно)" className="mt-1" value={filters.from} onChange={(value) => change('from', value)} />
          </label>
          <label className="text-xs text-muted">До (не включительно)
            <DateTimePicker label="До (не включительно)" className="mt-1" value={filters.to} onChange={(value) => change('to', value)} />
          </label>
        </div>
        <div className="mt-4 flex flex-wrap items-center gap-3">
          <button className="btn-ghost" onClick={() => { setFilters(empty); setCursors([]); setDownloadError('') }}>Сбросить</button>
          <button className="btn-primary" disabled={exporting || !!validationError} onClick={() => download('csv')}>Скачать CSV</button>
          <button className="btn-ghost" disabled={exporting || !!validationError} onClick={() => download('stix')}>Скачать STIX</button>
          <span className="text-xs text-muted">{exporting ? 'Подготовка файла…' : 'Экспорт всей выборки · даты в вашем часовом поясе'}</span>
        </div>
      </Card>
      {(validationError || downloadError || error) && <p role="alert" className="text-sm text-danger">{validationError || downloadError || (error as Error).message}</p>}

      <Card className="!p-0 overflow-hidden">
        {validationError || error ? <EmptyState title="Не удалось получить события" /> : isLoading ? (
          <Spinner />
        ) : events.length === 0 ? (
          <EmptyState title="Событий нет" />
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs text-muted border-b border-line/60">
                <th className="px-5 py-3 font-medium">Время</th>
                <th className="px-5 py-3 font-medium">Тип события</th>
                <th className="px-5 py-3 font-medium">Источник</th>
                <th className="px-5 py-3 font-medium">Назначение</th>
                <th className="px-5 py-3 font-medium">Тип ловушки</th>
              </tr>
            </thead>
            <tbody>
              {events.map((e) => (
                <tr
                  key={e.event_id}
                  onClick={() => setSelected(e.event_id)}
                  className="border-b border-line/40 last:border-0 hover:bg-bg-hover/40 cursor-pointer transition-colors"
                >
                  <td className="px-5 py-3 font-mono text-xs text-muted whitespace-nowrap">
                    <button type="button" className="text-left hover:text-brand focus-visible:underline"
                      aria-label={`Открыть событие ${e.event_type}, ${formatDateTime(e.occurred_at)}`}>
                      {formatDateTime(e.occurred_at)}
                    </button>
                  </td>
                  <td className="px-5 py-3">
                    <EventTypeBadge type={e.event_type} />
                  </td>
                  <td className="px-5 py-3 font-mono text-xs">
                    {e.source.ip}:{e.source.port}
                  </td>
                  <td className="px-5 py-3 font-mono text-xs text-muted">
                    {e.destination.protocol}:{e.destination.port}
                  </td>
                  <td className="px-5 py-3 font-mono text-xs text-muted">
                    {e.type_id}/{e.type_version}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>
      <div className="flex items-center gap-3">
        <button className="btn-ghost" disabled={cursors.length === 0 || isLoading} onClick={() => setCursors((previous) => previous.slice(0, -1))}>Назад</button>
        <span className="text-xs text-muted">Страница {cursors.length + 1} · {events.length} событий</span>
        <button className="btn-ghost" disabled={!data?.next_cursor || isLoading || !!validationError || !!error} onClick={() => { if (data?.next_cursor) setCursors((previous) => [...previous, data.next_cursor!]) }}>Далее</button>
      </div>

      <EventDetailModal id={selected} onClose={() => setSelected(null)} />
    </div>
  )
}
