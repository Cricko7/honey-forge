import { useState } from 'react'
import { Link } from 'react-router-dom'
import { Bug, ChevronRight, Plus } from 'lucide-react'
import { Card, ConnBadge, RuntimeBadge, LevelBadge, Spinner, EmptyState, LoadError } from '@/components/ui'
import { useCanManage, useTraps } from '@/api/hooks'
import { timeAgo } from '@/lib/utils'

const filters = [
  { key: undefined, label: 'Все' },
  { key: 'online', label: 'На связи' },
  { key: 'offline', label: 'Нет связи' },
] as const

export function TrapsPage() {
  const [conn, setConn] = useState<string | undefined>(undefined)
  const canManage = useCanManage()
  const { data, isLoading, error, refetch } = useTraps(conn ? { connectivity: conn } : undefined)
  const traps = data?.items ?? []

  return (
    <div className="page-content space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Ловушки</h1>
          <p className="text-sm text-muted">Связь с агентами и состояние ловушек.</p>
        </div>
        {canManage && <Link to="/app/network?create=1" className="btn-primary"><Plus size={16} /> Создать ловушку</Link>}
      </div>
        <div className="flex flex-wrap gap-1 border-b border-line" aria-label="Фильтр по связи">
          {filters.map((f) => (
            <button
              key={f.label}
              aria-pressed={conn === f.key}
              onClick={() => setConn(f.key)}
              className={`min-h-11 px-3 py-2 border-b-2 text-sm font-medium transition-colors ${
                conn === f.key ? 'border-brand text-brand' : 'border-transparent text-muted hover:text-fg'
              }`}
            >
              {f.label}
            </button>
          ))}
        </div>
      {error && <LoadError message={data ? 'Не удалось обновить ловушки. Показаны последние загруженные данные.' : 'Не удалось загрузить ловушки. Проверьте подключение.'} onRetry={() => { void refetch() }} />}
      <Card className="!p-0 overflow-x-auto">
        {isLoading ? (
          <Spinner />
        ) : !data && error ? null : traps.length === 0 ? (
          <EmptyState title={conn ? 'По этому фильтру ловушек нет' : 'Ловушек нет'} hint={conn ? 'Выберите «Все», чтобы увидеть остальные ловушки.' : canManage ? 'Нажмите «Создать ловушку», чтобы развернуть первую приманку.' : 'Ловушку может создать администратор организации.'} />
        ) : (
          <table className="data-table w-full text-sm">
            <thead>
              <tr className="text-left text-xs text-muted border-b border-line/60">
                <th className="px-5 py-3 font-medium">Ловушка</th>
                <th className="hidden lg:table-cell px-5 py-3 font-medium">Тип</th>
                <th className="hidden xl:table-cell px-5 py-3 font-medium">Уровень</th>
                <th className="hidden md:table-cell px-5 py-3 font-medium">Связь</th>
                <th className="px-5 py-3 font-medium">Состояние</th>
                <th className="hidden sm:table-cell px-5 py-3 font-medium">Последний сигнал</th>
                <th className="hidden sm:table-cell px-5 py-3"><span className="sr-only">Подробности</span></th>
              </tr>
            </thead>
            <tbody>
              {traps.map((t) => (
                <tr
                  key={t.id}
                  className="border-b border-line/40 last:border-0 hover:bg-bg-hover/40 transition-colors"
                >
                  <td className="px-5 py-3.5">
                    <Link to={`/app/traps/${t.id}`} className="flex min-h-11 items-center gap-2.5">
                      <div className="hidden sm:grid h-8 w-8 shrink-0 place-items-center">
                        <Bug size={15} className="text-muted" />
                      </div>
                      <div>
                        <div className="font-medium text-fg break-all">{t.name}</div>
                        <div className="text-xs text-muted break-all">{t.agent?.hostname ?? 'Сервер не определён'}</div>
                        <div className="sm:hidden mt-1 text-xs text-muted">{timeAgo(t.last_seen_at)}</div>
                      </div>
                    </Link>
                  </td>
                  <td className="hidden lg:table-cell px-5 py-3.5 font-mono text-xs text-muted">
                    {t.type_id}/{t.type_version}
                  </td>
                  <td className="hidden xl:table-cell px-5 py-3.5">
                    <LevelBadge level={t.interaction_level} />
                  </td>
                  <td className="hidden md:table-cell px-5 py-3.5">
                    <ConnBadge value={t.connectivity} />
                  </td>
                  <td className="px-5 py-3.5">
                    <RuntimeBadge value={t.runtime_state} />
                    <div className="md:hidden mt-1"><ConnBadge value={t.connectivity} /></div>
                  </td>
                  <td className="hidden sm:table-cell px-5 py-3.5 text-xs text-muted">{timeAgo(t.last_seen_at)}</td>
                  <td className="hidden sm:table-cell px-5 py-3.5 text-right">
                    <Link to={`/app/traps/${t.id}`} aria-label={`Открыть ловушку ${t.name}`} className="inline-flex min-h-11 min-w-11 items-center justify-center text-muted hover:text-brand">
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
