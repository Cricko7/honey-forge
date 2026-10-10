import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'
import type { Connectivity, RuntimeState, CommandStatus } from '@/types'

export function Card({ className, children }: { className?: string; children: ReactNode }) {
  return <div className={cn('card p-5', className)}>{children}</div>
}

export function SectionTitle({ children, action }: { children: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex items-center justify-between mb-4">
      <h2 className="text-sm font-semibold text-fg tracking-wide">{children}</h2>
      {action}
    </div>
  )
}

const dot = 'h-1.5 w-1.5 rounded-full'

export function ConnBadge({ value }: { value: Connectivity }) {
  return value === 'online' ? (
    <span className="chip bg-ok/10 text-ok">
      <span className={cn(dot, 'bg-ok animate-pulseGlow')} /> online
    </span>
  ) : (
    <span className="chip bg-muted/10 text-muted">
      <span className={cn(dot, 'bg-muted')} /> offline
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
  const label: Record<RuntimeState, string> = {
    running: 'работает',
    stopped: 'остановлена',
    error: 'ошибка',
    unknown: 'неизвестно',
  }
  return <span className={cn('chip', map[value])}>{label[value]}</span>
}

export function CommandStatusBadge({ value }: { value: CommandStatus }) {
  const map: Record<CommandStatus, string> = {
    queued: 'bg-info/10 text-info',
    running: 'bg-brand/15 text-brand-soft',
    succeeded: 'bg-ok/10 text-ok',
    failed: 'bg-danger/10 text-danger',
    expired: 'bg-muted/10 text-muted',
  }
  return <span className={cn('chip', map[value])}>{value}</span>
}

export function LevelBadge({ level }: { level: string }) {
  const map: Record<string, string> = {
    low: 'bg-info/10 text-info',
    medium: 'bg-warn/10 text-warn',
    high: 'bg-danger/10 text-danger',
  }
  return <span className={cn('chip uppercase', map[level] ?? 'bg-muted/10 text-muted')}>{level}</span>
}

export function EventTypeBadge({ type }: { type: string }) {
  const danger = type.includes('auth') || type.includes('payload') || type.includes('action')
  return (
    <span
      className={cn(
        'chip font-mono',
        danger ? 'bg-danger/10 text-danger' : 'bg-brand/10 text-brand-soft',
      )}
    >
      {type}
    </span>
  )
}

export function Spinner({ label }: { label?: string }) {
  return (
    <div className="flex items-center gap-3 text-muted text-sm py-8 justify-center">
      <span className="h-4 w-4 rounded-full border-2 border-brand/30 border-t-brand animate-spin" />
      {label ?? 'Загрузка…'}
    </div>
  )
}

export function EmptyState({ title, hint }: { title: string; hint?: string }) {
  return (
    <div className="text-center py-12 text-muted">
      <p className="text-sm font-medium text-fg">{title}</p>
      {hint && <p className="text-xs mt-1">{hint}</p>}
    </div>
  )
}
