import { ShieldAlert } from 'lucide-react'
import { Modal } from './Modal'
import { EmptyState, EventTypeBadge, Spinner } from './ui'
import { useEvent } from '@/api/hooks'
import { formatDateTime, flag, decodeBase64 } from '@/lib/utils'
import { RecordingView } from './RecordingView'
import { actionErrorMessage } from '@/lib/alerts'
import { ProcessExecutionDetails } from './ProcessExecutionDetails'

export function EventDetailModal({ id, onClose }: { id: string | null; onClose: () => void }) {
  const { data: event, isLoading, error, refetch } = useEvent(id ?? '')

  return (
    <Modal open={!!id} onClose={onClose} title="Детали события" width="max-w-xl">
      {error ? (
        <div role="alert" className="space-y-3 text-sm">
          <p className="text-danger">{actionErrorMessage(error)}</p>
          <button type="button" className="btn-ghost" onClick={() => refetch()}>Повторить</button>
        </div>
      ) : isLoading ? (
        <Spinner />
      ) : !event ? (
        <EmptyState title="Событие недоступно" />
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
          <div>
            <div className="flex items-center gap-1.5 text-xs text-danger mb-2">
              <ShieldAlert size={13} /> {event.event_type === 'process.exec' ? 'Телеметрия процесса' : 'Перехваченные данные'}
            </div>
            {event.event_type === 'process.exec' ? <ProcessExecutionDetails data={event.data} /> : <CapturedData data={event.data} />}
          </div>
          {event.type_id === 'ssh-high-interaction' && <RecordingView trapId={event.trap_id} sessionId={event.session_id} />}
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
  if ('input' in d) {
    const response = d.service === 'redis' && d.action_kind === 'request' && String(d.input).startsWith('Redis response: ')

    rows.push({ k: response ? 'Ответ Redis' : 'Команда', v: response ? String(d.input).slice('Redis response: '.length) : String(d.input), danger: true })
  }
  if ('outcome' in d) rows.push({ k: 'Результат', v: d.outcome === 'accepted' ? 'Успешно' : d.outcome === 'rejected' ? 'Отклонено' : d.outcome === 'unknown' ? 'Результат не определён' : String(d.outcome) })
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
