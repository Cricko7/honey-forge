import { memo } from 'react'
import { Handle, Position, type NodeProps } from 'reactflow'
import { Bug, Server, Database, Globe, ShieldCheck } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { Connectivity, RuntimeState } from '@/types'

export interface TrapNodeData {
  label: string
  typeId: string
  level: string
  connectivity: Connectivity
  runtime: RuntimeState
  port?: number
  underAttack?: boolean
  kind: 'trap' | 'gateway' | 'segment'
}

const typeIcon: Record<string, typeof Bug> = {
  'tcp-banner': Server,
  'redis-emulator': Database,
}

function TrapNodeInner({ data, selected }: NodeProps<TrapNodeData>) {
  if (data.kind === 'gateway') {
    return (
      <div className="relative">
        <Handle type="source" position={Position.Bottom} className="!bg-brand !w-2 !h-2" />
        <div className="flex flex-col items-center gap-1 px-5 py-3 rounded-2xl bg-bg-elevated border border-brand/40 shadow-glow">
          <Globe size={22} className="text-brand-soft" />
          <span className="text-xs font-semibold text-fg">{data.label}</span>
          <span className="text-[10px] text-muted">internet gateway</span>
        </div>
      </div>
    )
  }

  const Icon = typeIcon[data.typeId] ?? Bug
  const runtimeColor =
    data.runtime === 'running'
      ? 'text-ok'
      : data.runtime === 'error'
        ? 'text-danger'
        : data.runtime === 'stopped'
          ? 'text-muted'
          : 'text-warn'

  return (
    <div className="relative">
      <Handle type="target" position={Position.Top} className="!bg-line !w-2 !h-2" />
      <div
        className={cn(
          'w-48 rounded-2xl border bg-bg-panel/95 backdrop-blur-xl transition-all',
          selected ? 'border-brand shadow-glow' : 'border-line/70',
          data.underAttack && 'ring-2 ring-danger shadow-[0_0_28px_rgba(244,63,94,0.55)]',
        )}
      >
        <div className="flex items-center gap-2 px-3 py-2.5 border-b border-line/50">
          <div
            className={cn(
              'h-8 w-8 rounded-lg grid place-items-center shrink-0',
              data.underAttack ? 'bg-danger/20' : 'bg-brand/15',
            )}
          >
            <Icon size={16} className={data.underAttack ? 'text-danger' : 'text-brand-soft'} />
          </div>
          <div className="min-w-0">
            <div className="text-xs font-semibold text-fg truncate">{data.label}</div>
            <div className="text-[10px] text-muted font-mono truncate">
              {data.typeId}
              {data.port ? `:${data.port}` : ''}
            </div>
          </div>
        </div>
        <div className="flex items-center justify-between px-3 py-2 text-[10px]">
          <span className="flex items-center gap-1">
            <span
              className={cn(
                'h-1.5 w-1.5 rounded-full',
                data.connectivity === 'online' ? 'bg-ok animate-pulseGlow' : 'bg-muted',
              )}
            />
            <span className="text-muted">{data.connectivity}</span>
          </span>
          <span className={cn('flex items-center gap-1 font-medium', runtimeColor)}>
            <ShieldCheck size={11} /> {data.runtime}
          </span>
        </div>
        {data.underAttack && (
          <div className="absolute -top-2 -right-2 chip bg-danger text-white text-[9px] px-1.5 animate-pulseGlow">
            атака
          </div>
        )}
      </div>
      <span className={cn('absolute -bottom-1 left-1/2 -translate-x-1/2', 'text-[9px]')}>
        <span className="chip bg-bg-elevated text-muted uppercase">{data.level}</span>
      </span>
    </div>
  )
}

export const TrapNode = memo(TrapNodeInner)
