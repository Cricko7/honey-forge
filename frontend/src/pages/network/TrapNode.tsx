import { memo } from 'react'
import { Handle, Position, type NodeProps } from 'reactflow'
import { Bug, Server, Database, Globe } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { Connectivity, RuntimeState } from '@/types'
import { levelLabel, runtimeLabels } from '@/lib/ui-labels'

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
  'custom-http': Globe,
}

function TrapNodeInner({ data, selected, isConnectable }: NodeProps<TrapNodeData>) {
  if (data.kind === 'gateway') {
    return (
      <div className="relative">
        <Handle type="source" position={Position.Bottom} isConnectable={false} className="!bg-brand !w-2 !h-2" />
        <div className="flex flex-col items-center gap-1 px-5 py-3 rounded-lg bg-bg-elevated border border-line">
          <Globe size={22} className="text-brand-soft" />
          <span className="text-xs font-semibold text-fg">{data.label}</span>
          <span className="text-xs text-muted">Шлюз</span>
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
      {[Position.Top, Position.Right, Position.Bottom, Position.Left].map((side) => (
        <Handle key={side} id={side} type="source" position={side} isConnectable={isConnectable}
          title="Соединить ловушки: тяните от отправителя к получателю"
          aria-label={`Соединение ловушки: ${side}`}
          className="!bg-brand !w-3 !h-3 !z-10" />
      ))}
      <div
        className={cn(
          'w-52 rounded-xl border bg-bg-panel transition-colors',
          selected ? 'border-brand ring-1 ring-brand' : 'border-line',
          data.underAttack && 'ring-2 ring-danger',
        )}
      >
        <div className="flex items-center gap-2 px-3 py-2.5 border-b border-line/50">
          <div
            className={cn(
              'h-8 w-8 rounded-lg grid place-items-center shrink-0',
              data.underAttack ? 'bg-danger/10' : 'bg-brand/15',
            )}
          >
            <Icon size={16} className={data.underAttack ? 'text-danger' : 'text-brand-soft'} />
          </div>
          <div className="min-w-0">
            <div className="text-xs font-semibold text-fg truncate">{data.label}</div>
            <div className="text-xs text-muted font-mono truncate">
              {data.typeId}
              {data.port ? `:${data.port}` : ''}
            </div>
            <div className="text-xs text-muted">{levelLabel(data.level)}</div>
          </div>
        </div>
        <div className="flex items-center justify-between gap-3 px-3 py-2.5 text-xs">
          <span className="flex items-center gap-1">
            <span
              className={cn(
                'h-1.5 w-1.5 rounded-full',
                data.connectivity === 'online' ? 'bg-ok' : 'bg-muted',
              )}
            />
            <span className="text-muted">{data.connectivity === 'online' ? 'На связи' : 'Нет связи'}</span>
          </span>
          <span className={cn('flex items-center gap-1 font-medium', runtimeColor)}>
            {runtimeLabels[data.runtime]}
          </span>
        </div>
        {data.underAttack && (
          <div className="absolute -top-2 -right-2 chip bg-danger text-white text-[9px] px-1.5">
            атака
          </div>
        )}
      </div>
    </div>
  )
}

export const TrapNode = memo(TrapNodeInner)
