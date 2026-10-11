import { useState } from 'react'
import { Card, EventTypeBadge, Spinner, EmptyState } from '@/components/ui'
import { EventDetailModal } from '@/components/EventDetailModal'
import { useEvents } from '@/api/hooks'
import { formatDateTime } from '@/lib/utils'

export function EventsPage() {
  const { data, isLoading } = useEvents({ limit: 100 })
  const [selected, setSelected] = useState<string | null>(null)
  const events = data?.items ?? []

  return (
    <div className="p-6 space-y-5">
      <div>
        <h1 className="text-xl font-semibold">События</h1>
        <p className="text-sm text-muted">
          Журнал активности атакующих · перехваченные подключения, креды и команды
        </p>
      </div>

      <Card className="!p-0 overflow-hidden">
        {isLoading ? (
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

      <EventDetailModal id={selected} onClose={() => setSelected(null)} />
    </div>
  )
}
