import { useState } from 'react'
import { Link } from 'react-router-dom'
import { Bug, ChevronRight } from 'lucide-react'
import { Card, ConnBadge, RuntimeBadge, LevelBadge, Spinner, EmptyState } from '@/components/ui'
import { useTraps } from '@/api/hooks'
import { timeAgo } from '@/lib/utils'

const filters = [
  { key: undefined, label: 'Все' },
  { key: 'online', label: 'Online' },
  { key: 'offline', label: 'Offline' },
] as const

export function TrapsPage() {
  const [conn, setConn] = useState<string | undefined>(undefined)
  const { data, isLoading } = useTraps(conn ? { connectivity: conn } : undefined)
  const traps = data?.items ?? []

  return (
    <div className="p-6 space-y-5">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">Ловушки</h1>
          <p className="text-sm text-muted">Зарегистрированные honeypot-приманки организации</p>
        </div>
        <div className="flex gap-1 bg-bg-elevated rounded-xl p-1">
          {filters.map((f) => (
            <button
              key={f.label}
              onClick={() => setConn(f.key)}
              className={`px-3 py-1.5 rounded-lg text-xs font-medium transition-colors ${
                conn === f.key ? 'bg-brand text-white' : 'text-muted hover:text-fg'
              }`}
            >
              {f.label}
            </button>
          ))}
        </div>
      </div>

      <Card className="!p-0 overflow-hidden">
        {isLoading ? (
          <Spinner />
        ) : traps.length === 0 ? (
          <EmptyState title="Ловушек нет" hint="Добавьте ловушку в конструкторе сети" />
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs text-muted border-b border-line/60">
                <th className="px-5 py-3 font-medium">Ловушка</th>
                <th className="px-5 py-3 font-medium">Тип</th>
                <th className="px-5 py-3 font-medium">Уровень</th>
                <th className="px-5 py-3 font-medium">Связь</th>
                <th className="px-5 py-3 font-medium">Состояние</th>
                <th className="px-5 py-3 font-medium">Последний сигнал</th>
                <th className="px-5 py-3"></th>
              </tr>
            </thead>
            <tbody>
              {traps.map((t) => (
                <tr
                  key={t.id}
                  className="border-b border-line/40 last:border-0 hover:bg-bg-hover/40 transition-colors"
                >
                  <td className="px-5 py-3.5">
                    <Link to={`/app/traps/${t.id}`} className="flex items-center gap-2.5">
                      <div className="h-8 w-8 rounded-lg bg-brand/15 grid place-items-center">
                        <Bug size={15} className="text-brand-soft" />
                      </div>
                      <div>
                        <div className="font-medium text-fg">{t.name}</div>
                        <div className="text-xs text-muted">{t.agent?.hostname ?? '—'}</div>
                      </div>
                    </Link>
                  </td>
                  <td className="px-5 py-3.5 font-mono text-xs text-muted">
                    {t.type_id}/{t.type_version}
                  </td>
                  <td className="px-5 py-3.5">
                    <LevelBadge level={t.interaction_level} />
                  </td>
                  <td className="px-5 py-3.5">
                    <ConnBadge value={t.connectivity} />
                  </td>
                  <td className="px-5 py-3.5">
                    <RuntimeBadge value={t.runtime_state} />
                  </td>
                  <td className="px-5 py-3.5 text-xs text-muted">{timeAgo(t.last_seen_at)}</td>
                  <td className="px-5 py-3.5 text-right">
                    <Link to={`/app/traps/${t.id}`} className="text-muted hover:text-brand-soft">
                      <ChevronRight size={16} />
                    </Link>
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
