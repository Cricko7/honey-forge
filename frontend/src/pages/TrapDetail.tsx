import { Link, useParams } from 'react-router-dom'
import {
  ArrowLeft,
  Play,
  Square,
  RefreshCw,
  Trash2,
  Server,
  Activity,
  KeyRound,
} from 'lucide-react'
import {
  Card,
  SectionTitle,
  ConnBadge,
  RuntimeBadge,
  LevelBadge,
  CommandStatusBadge,
  EventTypeBadge,
  Spinner,
  EmptyState,
} from '@/components/ui'
import {
  useTrap,
  useCommands,
  useEvents,
  useSendCommand,
  useDeleteTrap,
} from '@/api/hooks'
import { formatDateTime, timeAgo } from '@/lib/utils'

export function TrapDetailPage() {
  const { id = '' } = useParams()
  const { data: trap, isLoading } = useTrap(id)
  const { data: commands } = useCommands(id)
  const { data: events } = useEvents({ trap_id: id, limit: 20 })
  const sendCommand = useSendCommand()
  const deleteTrap = useDeleteTrap()

  if (isLoading) return <div className="p-6"><Spinner /></div>
  if (!trap) return <div className="p-6"><EmptyState title="Ловушка не найдена" /></div>

  const busy = sendCommand.isPending || !!trap.active_command_id

  return (
    <div className="p-6 space-y-5">
      <Link to="/app/traps" className="inline-flex items-center gap-1.5 text-sm text-muted hover:text-fg">
        <ArrowLeft size={15} /> К списку ловушек
      </Link>

      <div className="flex items-start justify-between flex-wrap gap-4">
        <div className="flex items-center gap-3">
          <div className="h-12 w-12 rounded-2xl bg-brand/15 grid place-items-center">
            <Server size={22} className="text-brand-soft" />
          </div>
          <div>
            <h1 className="text-xl font-semibold">{trap.name}</h1>
            <div className="flex items-center gap-2 mt-1">
              <span className="font-mono text-xs text-muted">
                {trap.type_id}/{trap.type_version}
              </span>
              <LevelBadge level={trap.interaction_level} />
              <ConnBadge value={trap.connectivity} />
              <RuntimeBadge value={trap.runtime_state} />
            </div>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <button
            className="btn-ghost"
            disabled={busy}
            onClick={() => sendCommand.mutate({ trapId: id, action: 'start' })}
          >
            <Play size={15} /> Запустить
          </button>
          <button
            className="btn-ghost"
            disabled={busy}
            onClick={() => sendCommand.mutate({ trapId: id, action: 'stop' })}
          >
            <Square size={15} /> Остановить
          </button>
          <button
            className="btn-ghost"
            disabled={busy}
            onClick={() => sendCommand.mutate({ trapId: id, action: 'apply_config' })}
          >
            <RefreshCw size={15} /> Применить конфиг
          </button>
          <button
            className="btn-danger"
            onClick={() => {
              if (confirm('Удалить ловушку?')) deleteTrap.mutate({ id, revision: trap.revision })
            }}
          >
            <Trash2 size={15} />
          </button>
        </div>
      </div>

      {busy && (
        <div className="chip bg-brand/15 text-brand-soft animate-pulseGlow">
          <Activity size={12} /> выполняется команда…
        </div>
      )}

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        {/* Параметры */}
        <Card>
          <SectionTitle>Параметры</SectionTitle>
          <dl className="space-y-2.5 text-sm">
            <Row label="Revision" value={String(trap.revision)} mono />
            <Row label="State version" value={String(trap.state_version)} mono />
            <Row label="Desired state" value={trap.desired_state} />
            <Row
              label="Applied revision"
              value={trap.applied_profile_revision?.toString() ?? '—'}
              mono
            />
            <Row label="Создана" value={formatDateTime(trap.created_at)} />
            <Row label="Последний сигнал" value={timeAgo(trap.last_seen_at)} />
          </dl>
        </Card>

        {/* Агент */}
        <Card>
          <SectionTitle
            action={<KeyRound size={14} className="text-muted" />}
          >
            Агент
          </SectionTitle>
          {trap.agent ? (
            <dl className="space-y-2.5 text-sm">
              <Row label="Hostname" value={trap.agent.hostname} mono />
              <Row label="Версия" value={trap.agent.agent_version} mono />
              <Row label="Буфер событий" value={String(trap.agent.buffered_events)} />
              <Row
                label="Буфер"
                value={`${(trap.agent.buffer_bytes / 1024).toFixed(1)} / ${(
                  trap.agent.buffer_capacity_bytes / 1024
                ).toFixed(0)} KiB`}
              />
              <Row label="Состояние буфера" value={trap.agent.buffer_state} />
            </dl>
          ) : (
            <EmptyState
              title="Агент не подключён"
              hint="Выдайте реквизиты агента и подключите его к ловушке"
            />
          )}
        </Card>

        {/* Команды */}
        <Card>
          <SectionTitle>История команд</SectionTitle>
          <div className="space-y-2 max-h-64 overflow-auto">
            {(commands?.items ?? []).length === 0 ? (
              <EmptyState title="Команд пока нет" />
            ) : (
              commands!.items.map((c) => (
                <div
                  key={c.id}
                  className="flex items-center justify-between text-xs p-2.5 rounded-lg bg-bg-elevated/50"
                >
                  <span className="font-mono text-fg">{c.action}</span>
                  <CommandStatusBadge value={c.status} />
                </div>
              ))
            )}
          </div>
        </Card>
      </div>

      {/* События этой ловушки */}
      <Card className="!p-0 overflow-hidden">
        <div className="px-5 py-4 border-b border-line/60">
          <SectionTitle>Последние события</SectionTitle>
        </div>
        {(events?.items ?? []).length === 0 ? (
          <EmptyState title="Событий нет" />
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs text-muted border-b border-line/60">
                <th className="px-5 py-2.5 font-medium">Время</th>
                <th className="px-5 py-2.5 font-medium">Тип</th>
                <th className="px-5 py-2.5 font-medium">Источник</th>
                <th className="px-5 py-2.5 font-medium">Назначение</th>
              </tr>
            </thead>
            <tbody>
              {events!.items.map((e) => (
                <tr key={e.event_id} className="border-b border-line/40 last:border-0">
                  <td className="px-5 py-2.5 font-mono text-xs text-muted">
                    {formatDateTime(e.occurred_at)}
                  </td>
                  <td className="px-5 py-2.5">
                    <EventTypeBadge type={e.event_type} />
                  </td>
                  <td className="px-5 py-2.5 font-mono text-xs">
                    {e.source.ip}:{e.source.port}
                  </td>
                  <td className="px-5 py-2.5 font-mono text-xs text-muted">
                    {e.destination.protocol}:{e.destination.port}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>
    </div>
  )
}

function Row({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-center justify-between">
      <dt className="text-muted">{label}</dt>
      <dd className={mono ? 'font-mono text-fg' : 'text-fg'}>{value}</dd>
    </div>
  )
}
