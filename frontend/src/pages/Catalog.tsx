import { Card, LevelBadge, Spinner, LoadError, EmptyState } from '@/components/ui'
import { useCatalog } from '@/api/hooks'
import { commandLabels, eventLabel } from '@/lib/ui-labels'
import type { CommandAction } from '@/types'

export function CatalogPage() {
  const { data, isLoading, error, refetch } = useCatalog()
  const entries = data?.items ?? []

  return (
    <div className="page-content space-y-5">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Каталог типов ловушек</h1>
        <p className="text-sm text-muted">
          Готовые типы ловушек: поведение, события и доступные действия.
        </p>
      </div>

      {error && <LoadError message="Не удалось обновить каталог типов ловушек." onRetry={() => { void refetch() }} />}
      {isLoading ? (
        <Spinner />
      ) : !data && error ? null : !entries.length ? <EmptyState title="Типов ловушек пока нет" /> : (
        <div>
          {entries.map((c) => (
            <Card key={`${c.type_id}/${c.type_version}`} className="grid xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)] gap-x-10">
              <div>
              <div className="flex flex-wrap items-start justify-between gap-3 mb-3">
                <div className="min-w-0 break-words">
                  <h2 className="font-semibold text-fg">{c.title}</h2>
                  <span className="font-mono text-xs text-muted">
                    {c.type_id}/{c.type_version}
                  </span>
                </div>
                <div className="flex items-center gap-2">
                  <LevelBadge level={c.interaction_level} />
                  {c.available_for_new_profiles && (
                    <span className="text-xs text-ok">Доступен</span>
                  )}
                </div>
              </div>
              <p className="text-sm text-muted mb-4">{c.description}</p>
              </div>

              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <div>
                  <div className="flex items-center gap-1.5 text-xs text-fg mb-2">
                    События
                  </div>
                  <div className="space-y-1">
                    {c.event_schemas.map((e) => (
                      <div key={e.event_type} title={e.event_type} className="text-sm text-muted break-words">
                        {eventLabel(e.event_type)}
                      </div>
                    ))}
                  </div>
                </div>
                <div>
                  <div className="flex items-center gap-1.5 text-xs text-fg mb-2">
                    Действия
                  </div>
                  <div className="space-y-1">
                    {c.actions.map((a) => (
                      <div key={a.action} title={a.action} className="text-sm text-muted">
                        {commandLabels[a.action as CommandAction] ?? a.title ?? a.action}
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}
