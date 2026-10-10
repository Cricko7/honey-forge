import { useState } from 'react'
import { ScrollText, ShieldAlert } from 'lucide-react'
import { Card, EventTypeBadge, Spinner, EmptyState } from '@/components/ui'
import { Modal } from '@/components/Modal'
import { useEvents, useEvent } from '@/api/hooks'
import { formatDateTime, flag, decodeBase64 } from '@/lib/utils'

export function EventsPage() {
  const { data, isLoading } = useEvents({ limit: 100 })
  const [selected, setSelected] = useState<string | null>(null)
  const events = data?.items ?? []

  return (
    <div className="p-6 space-y-5">
      <div className="flex items-center gap-3">
        <div className="h-10 w-10 rounded-xl bg-brand/15 grid place-items-center">
          <ScrollText size={18} className="text-brand-soft" />
        </div>
        <div>
          <h1 className="text-xl font-semibold">События</h1>
          <p className="text-sm text-muted">
            Журнал активности атакующих · перехваченные подключения, креды и команды
          </p>
        </div>
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
                    {formatDateTime(e.occurred_at)}
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

function EventDetailModal({ id, onClose }: { id: string | null; onClose: () => void }) {
  const { data: event, isLoading } = useEvent(id ?? '')

  return (
    <Modal open={!!id} onClose={onClose} title="Детали события" width="max-w-xl">
      {isLoading || !event ? (
        <Spinner />
      ) : (
        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <EventTypeBadge type={event.event_type} />
            <span className="text-xs text-muted font-mono">{formatDateTime(event.occurred_at)}</span>
          </div>

          <div className="grid grid-cols-2 gap-3 text-sm">
            <Field label="Источник">
              <span className="font-mono">
                {flag(event.source_enrichment.country_code)} {event.source.ip}:{event.source.port}
              </span>
            </Field>
            <Field label="Страна / ASN">
              <span className="font-mono">
                {event.source_enrichment.country_code ?? '—'} · AS{event.source_enrichment.asn ?? '—'}
              </span>
            </Field>
            <Field label="Назначение">
              <span className="font-mono">
                {event.destination.protocol}:{event.destination.port}
              </span>
            </Field>
            <Field label="Сессия">
              <span className="font-mono text-xs">#{event.session_sequence}</span>
            </Field>
          </div>

          {/* Перехваченные данные атакующего */}
          <div>
            <div className="flex items-center gap-1.5 text-xs text-danger mb-2">
              <ShieldAlert size={13} /> Перехваченные данные
            </div>
            <CapturedData data={event.data} />
          </div>
        </div>
      )}
    </Modal>
  )
}

function CapturedData({ data }: { data: Record<string, unknown> }) {
  const d = data as Record<string, unknown>
  const rows: { k: string; v: string; danger?: boolean }[] = []

  if ('username' in d) rows.push({ k: 'Логин', v: String(d.username), danger: true })
  if ('password' in d) rows.push({ k: 'Пароль', v: String(d.password), danger: true })
  if ('input' in d) rows.push({ k: 'Команда', v: String(d.input), danger: true })
  if ('outcome' in d) rows.push({ k: 'Результат', v: String(d.outcome) })
  if ('payload_base64' in d) {
    rows.push({ k: 'Payload (decoded)', v: decodeBase64(String(d.payload_base64)), danger: true })
  }
  if ('listener_name' in d) rows.push({ k: 'Listener', v: String(d.listener_name) })
  if ('reason' in d) rows.push({ k: 'Причина', v: String(d.reason) })
  if ('duration_ms' in d) rows.push({ k: 'Длительность', v: `${d.duration_ms} мс` })
  if ('bytes_received' in d) rows.push({ k: 'Байт получено', v: String(d.bytes_received) })

  if (rows.length === 0) {
    return (
      <pre className="text-xs bg-bg-elevated rounded-xl p-3 font-mono text-muted overflow-auto">
        {JSON.stringify(data, null, 2)}
      </pre>
    )
  }

  return (
    <div className="space-y-1.5">
      {rows.map((r) => (
        <div
          key={r.k}
          className="flex items-center justify-between text-sm px-3 py-2 rounded-lg bg-bg-elevated/60"
        >
          <span className="text-muted text-xs">{r.k}</span>
          <span className={`font-mono text-xs break-all ${r.danger ? 'text-danger' : 'text-fg'}`}>
            {r.v}
          </span>
        </div>
      ))}
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="bg-bg-elevated/50 rounded-xl p-3">
      <div className="text-xs text-muted mb-1">{label}</div>
      <div className="text-fg">{children}</div>
    </div>
  )
}
