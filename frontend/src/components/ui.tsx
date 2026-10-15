import { severityLabel, type EventSeverity } from '@/lib/event-severity'
import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'
import type { Connectivity, RuntimeState, CommandStatus } from '@/types'
import { commandStatusLabels, eventLabel, levelLabel, runtimeLabels } from '@/lib/ui-labels'

export function Card({ className, children }: { className?: string; children: ReactNode }) {
  return <div className={cn('min-w-0 border-t border-line py-5', className)}>{children}</div>
}

export function SectionTitle({ children, action }: { children: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 mb-4">
      <h2 className="min-w-0 break-words text-base font-semibold text-fg">{children}</h2>
      {action}
    </div>
  )
}

const dot = 'h-1.5 w-1.5 rounded-full'

export function ConnBadge({ value }: { value: Connectivity }) {
  return value === 'online' ? (
    <span className="chip bg-ok/10 text-ok">
      <span className={cn(dot, 'bg-ok')} /> На связи
    </span>
  ) : (
    <span className="chip bg-muted/10 text-muted">
      <span className={cn(dot, 'bg-muted')} /> Нет связи
    </span>
  )
}

export function RuntimeBadge({ value }: { value: RuntimeState }) {
  const map: Record<RuntimeState, string> = {
    running: 'bg-ok/10 text-ok',
    stopped: 'bg-muted/10 text-muted',
    error: 'bg-danger/10 text-danger',
    unknown: 'bg-warn/10 text-warn',
  }
  return <span className={cn('chip', map[value])}>{runtimeLabels[value]}</span>
}

export function CommandStatusBadge({ value }: { value: CommandStatus }) {
  const map: Record<CommandStatus, string> = {
    queued: 'bg-info/10 text-info',
    running: 'bg-brand/15 text-brand',
    succeeded: 'bg-ok/10 text-ok',
    failed: 'bg-danger/10 text-danger',
    expired: 'bg-muted/10 text-muted',
  }
  return <span title={value} className={cn('chip', map[value])}>{commandStatusLabels[value]}</span>
}

export function LevelBadge({ level }: { level: string }) {
  return <span title={`Уровень взаимодействия: ${levelLabel(level)}`} className="text-xs text-muted">{levelLabel(level)}</span>
}

export function EventTypeBadge({ type, severity }: { type: string; severity?: EventSeverity }) {
  return (
    <span
      className={`text-sm ${severity ? `event-label-${severity}` : "text-fg"}`}
      title={type}
    >
      {eventLabel(type)}
      {severity && <span className="ml-2 text-xs font-medium">{severityLabel(severity)}</span>}
    </span>
  )
}

export function Spinner({ label }: { label?: string }) {
  return (
    <div role="status" className="flex items-center gap-3 text-muted text-sm py-8 justify-center">
      <span aria-hidden="true" className="h-4 w-4 rounded-full border-2 border-brand/30 border-t-brand animate-spin" />
      {label ?? 'Загрузка…'}
    </div>
  )
}

export function LoadError({ message, onRetry }: { message: string; onRetry: () => void }) {
  return <div role="alert" className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-danger/30 bg-bg-panel p-4 text-sm">
    <p>{message}</p>
    <button type="button" className="btn-ghost" onClick={onRetry}>Повторить</button>
  </div>
}

export function EmptyState({ title, hint }: { title: string; hint?: string }) {
  return (
    <div className="mx-auto max-w-lg px-5 py-14 text-center text-muted">
      <p className="text-sm font-medium text-fg">{title}</p>
      {hint && <p className="text-sm leading-relaxed mt-2">{hint}</p>}
    </div>
  )
}
