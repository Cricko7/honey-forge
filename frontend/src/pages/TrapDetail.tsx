import { eventSeverity } from '@/lib/event-severity'
import { SourceGeo } from '@/components/SourceGeo'
import { useState } from 'react'
import { EventDetailModal } from '@/components/EventDetailModal'
import { Link, useNavigate, useParams } from 'react-router-dom'
import {
  ArrowLeft,
  Play,
  Square,
  RefreshCw,
  Trash2,
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
  LoadError,
} from '@/components/ui'
import {
  useCanManageTraps,
  useTrap,
  useCommands,
  useEvents,
  useProfiles,
  useSendCommand,
  useDeleteTrap,
} from '@/api/hooks'
import { ApiError } from '@/api/client'
import { formatDateTime, timeAgo } from '@/lib/utils'
import { commandControls } from '@/lib/command-controls'
import { actionErrorMessage } from '@/lib/alerts'
import { commandLabels, runtimeLabels } from '@/lib/ui-labels'

export function TrapDetailPage() {
  const [selectedEvent, setSelectedEvent] = useState<string | null>(null)
  const canManage = useCanManageTraps()
  const navigate = useNavigate()
  const { id = '' } = useParams()
  const { data: trap, isLoading, error, refetch } = useTrap(id)
  const commandsQuery = useCommands(id)
  const eventsQuery = useEvents({ trap_id: id, limit: 20 })
  const commands = commandsQuery.data
  const events = eventsQuery.data
  const { data: profiles } = useProfiles()
  const sendCommand = useSendCommand()
  const deleteTrap = useDeleteTrap()

  if (isLoading) return <div className="p-6"><Spinner /></div>
  if (error && !trap && !('status' in error && error.status === 404)) return <div className="p-6"><LoadError message="Не удалось загрузить ловушку. Проверьте подключение." onRetry={() => { void refetch() }} /></div>
  if (!trap) return <div className="p-6"><EmptyState title="Ловушка не найдена" /></div>

  const controls = commandControls(trap, commands?.items ?? [], sendCommand.isPending)
  const busy = controls.disabled || deleteTrap.isPending
  const activeCommand = commands?.items.find((c) => c.id === trap.active_command_id)
  // apply_config валидируется под дескриптор действия: нужна точная ревизия профиля.
  const profileRevision =
    profiles?.items.find((p) => p.id === trap.profile_id)?.revision ??
    trap.applied_profile_revision ??
    trap.revision
  const cmdError = sendCommand.error as ApiError | null

  return (
    <div className="page-content space-y-5">
      {error && <LoadError message="Не удалось обновить ловушку. Показаны последние загруженные данные." onRetry={() => { void refetch() }} />}
      <Link to="/app/traps" className="inline-flex min-h-11 items-center gap-1.5 text-sm text-muted hover:text-fg">
        <ArrowLeft size={15} /> К списку ловушек
      </Link>

      <div className="flex items-start justify-between flex-wrap gap-4">
        <div className="flex items-center gap-3 min-w-0">
          <div className="min-w-0">
            <h1 className="text-2xl font-semibold tracking-tight break-words">{trap.name}</h1>
            <div className="flex flex-wrap items-center gap-2 mt-1">
              <span className="font-mono text-xs text-muted break-all">
                {trap.type_id}/{trap.type_version}
              </span>
              <LevelBadge level={trap.interaction_level} />
              <ConnBadge value={trap.connectivity} />
              <RuntimeBadge value={trap.runtime_state} />
            </div>
          </div>
        </div>

        {canManage && <div className="flex flex-wrap items-center gap-2">
          <button
            className="btn-ghost"
            disabled={busy}
            onClick={() => sendCommand.mutate({ trapId: id, action: 'start' })}
          >
            <Play size={15} /> Запустить
          </button>
          <button
            className="btn-ghost"
            disabled={busy || trap.runtime_state === 'stopped'}
            onClick={() => sendCommand.mutate({ trapId: id, action: 'stop' })}
          >
            <Square size={15} /> Остановить
          </button>
          <button
            className="btn-ghost"
            disabled={busy}
            onClick={() =>
              sendCommand.mutate({
                trapId: id,
                action: 'apply_config',
                params: { profile_revision: profileRevision },
              })
            }
          >
            <RefreshCw size={15} /> Применить настройки
          </button>
          <button
            className="btn-danger"
            aria-label="Удалить ловушку"
            title={deleteTrap.isPending ? 'Удаление…' : 'Удалить ловушку'}
            disabled={deleteTrap.isPending}
            onClick={() => {
              if (deleteTrap.isPending) return
              if (confirm(trap.connectivity === 'offline'
                ? 'Удалить offline-ловушку? Незавершённые команды будут закрыты, а несохранённые события агента могут быть потеряны.'
                : 'Удалить ловушку?')) deleteTrap.mutate(
                { id, revision: trap.revision },
                { onSuccess: () => navigate('/app/traps', { replace: true }) },
              )
            }}
          >
            <Trash2 size={15} />
          </button>
        </div>}
      </div>

      {deleteTrap.error && (
        <div role="alert" className="chip bg-danger/10 text-danger">
          Не удалось удалить ловушку. {actionErrorMessage(deleteTrap.error)}
        </div>
      )}
      {controls.message && !deleteTrap.isPending && (
        <div role="status" className="chip bg-brand/15 text-brand-soft">
          <Activity size={12} /> {controls.message}
          {activeCommand?.status === 'queued' && ` Срок ожидания: до ${formatDateTime(activeCommand.expires_at)}.`}
        </div>
      )}
      {cmdError && !sendCommand.isPending && (
        <div role="alert" className="chip bg-danger/10 text-danger">
          Команда отклонена: {cmdError.code === 'configuration_not_applied'
            ? 'сначала нажмите «Применить настройки»'
            : actionErrorMessage(cmdError)}
        </div>
      )}

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <Card>
          <SectionTitle>Параметры</SectionTitle>
          <dl className="space-y-2.5 text-sm">
            <Row label="Сервер развёртывания" value={trap.deployment_node_id || 'Сервер платформы'} mono />
            <Row label="Ожидаемое состояние" value={runtimeLabels[trap.desired_state]} />
            <Row label="Создана" value={formatDateTime(trap.created_at)} />
            <Row label="Последний сигнал" value={timeAgo(trap.last_seen_at)} />
          </dl>
          <details className="mt-3"><summary className="min-h-11 cursor-pointer text-sm text-brand">Технические сведения</summary>
            <dl className="space-y-2.5 text-sm mt-2">
              <Row label="Версия ловушки" value={String(trap.revision)} mono />
              <Row label="Версия состояния" value={String(trap.state_version)} mono />
              <Row label="Применённая версия профиля" value={trap.applied_profile_revision?.toString() ?? '—'} mono />
            </dl>
          </details>
        </Card>
        <Card>
          <SectionTitle
            action={<KeyRound size={14} className="text-muted" />}
          >
            Агент
          </SectionTitle>
          {trap.agent ? (
            <dl className="space-y-2.5 text-sm">
              <Row label="Имя сервера" value={trap.agent.hostname} mono />
              <Row label="Версия" value={trap.agent.agent_version} mono />
              <Row label="Буфер событий" value={String(trap.agent.buffered_events)} />
              <Row
                label="Буфер"
                value={`${(trap.agent.buffer_bytes / 1024).toFixed(1)} / ${(
                  trap.agent.buffer_capacity_bytes / 1024
                ).toFixed(0)} KiB`}
              />
              <Row label="Состояние буфера" value={{ ok: 'В норме', full: 'Заполнен', unavailable: 'Недоступен' }[trap.agent.buffer_state]} />
            </dl>
          ) : (
            <EmptyState
              title="Агент не подключён"
              hint="Выдайте реквизиты агента и подключите его к ловушке"
            />
          )}
        </Card>
        <Card>
          <SectionTitle>История команд</SectionTitle>
          {commandsQuery.error && <LoadError message="Не удалось обновить историю команд." onRetry={() => { void commandsQuery.refetch() }} />}
          <div className="divide-y divide-line max-h-64 overflow-auto">
            {commandsQuery.isLoading ? <Spinner /> : !commands && commandsQuery.error ? null : (commands?.items ?? []).length === 0 ? (
              <EmptyState title="Команд пока нет" />
            ) : (
              commands!.items.map((c) => (
                <div
                  key={c.id}
                  className="flex items-center justify-between gap-3 text-xs py-2.5"
                >
                  <span className="text-fg">{commandLabels[c.action]}</span>
                  <CommandStatusBadge value={c.status} />
                </div>
              ))
            )}
          </div>
        </Card>
      </div>

      <Card className="!p-0 overflow-x-auto">
        <div className="px-5 py-4 border-b border-line/60">
          <SectionTitle>Последние события</SectionTitle>
        </div>
        {eventsQuery.error && <LoadError message="Не удалось обновить события ловушки." onRetry={() => { void eventsQuery.refetch() }} />}
        {eventsQuery.isLoading ? <Spinner /> : !events && eventsQuery.error ? null : (events?.items ?? []).length === 0 ? (
          <EmptyState title="Событий нет" />
        ) : (
          <table className="data-table w-full min-w-[640px] text-sm">
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
                <tr key={e.event_id} onClick={() => setSelectedEvent(e.event_id)}
                  className="border-b border-line/40 last:border-0 hover:bg-bg-hover/40 cursor-pointer transition-colors">
                  <td className="px-5 py-2.5 font-mono text-xs text-muted">
                    <button type="button" className="text-left hover:text-brand focus-visible:underline"
                      aria-label={`Открыть событие ${e.event_type}, ${formatDateTime(e.occurred_at)}`}>
                      {formatDateTime(e.occurred_at)}
                    </button>
                  </td>
                  <td className="px-5 py-2.5">
                    <EventTypeBadge type={e.event_type} severity={eventSeverity(e)} />
                  </td>
                  <td className="px-5 py-2.5 font-mono text-xs">
                    {e.source.ip}:{e.source.port}
                    <SourceGeo enrichment={e.source_enrichment} />
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
      <EventDetailModal id={selectedEvent} onClose={() => setSelectedEvent(null)} />
    </div>
  )
}

function Row({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
      <dt className="text-muted">{label}</dt>
      <dd className={`min-w-0 break-words ${mono ? 'font-mono text-fg' : 'text-fg'}`}>{value}</dd>
    </div>
  )
}
