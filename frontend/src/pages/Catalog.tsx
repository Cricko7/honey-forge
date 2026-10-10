import { Boxes, Zap, Terminal } from 'lucide-react'
import { Card, LevelBadge, Spinner } from '@/components/ui'
import { useCatalog } from '@/api/hooks'

export function CatalogPage() {
  const { data, isLoading } = useCatalog()
  const entries = data?.items ?? []

  return (
    <div className="p-6 space-y-5">
      <div className="flex items-center gap-3">
        <div className="h-10 w-10 rounded-xl bg-brand/15 grid place-items-center">
          <Boxes size={18} className="text-brand-soft" />
        </div>
        <div>
          <h1 className="text-xl font-semibold">Каталог типов ловушек</h1>
          <p className="text-sm text-muted">
            Immutable-типы с версиями · описывают конфиг, события и действия
          </p>
        </div>
      </div>

      {isLoading ? (
        <Spinner />
      ) : (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          {entries.map((c) => (
            <Card key={`${c.type_id}/${c.type_version}`}>
              <div className="flex items-start justify-between mb-3">
                <div>
                  <h2 className="font-semibold text-fg">{c.title}</h2>
                  <span className="font-mono text-xs text-muted">
                    {c.type_id}/{c.type_version}
                  </span>
                </div>
                <div className="flex items-center gap-2">
                  <LevelBadge level={c.interaction_level} />
                  {c.available_for_new_profiles && (
                    <span className="chip bg-ok/10 text-ok">доступен</span>
                  )}
                </div>
              </div>
              <p className="text-sm text-muted mb-4">{c.description}</p>

              <div className="grid grid-cols-2 gap-3">
                <div>
                  <div className="flex items-center gap-1.5 text-xs text-fg mb-2">
                    <Zap size={12} className="text-brand-soft" /> События
                  </div>
                  <div className="space-y-1">
                    {c.event_schemas.map((e) => (
                      <div key={e.event_type} className="text-xs font-mono text-muted">
                        {e.event_type}
                      </div>
                    ))}
                  </div>
                </div>
                <div>
                  <div className="flex items-center gap-1.5 text-xs text-fg mb-2">
                    <Terminal size={12} className="text-brand-soft" /> Действия
                  </div>
                  <div className="flex flex-wrap gap-1.5">
                    {c.actions.map((a) => (
                      <span key={a.action} className="chip bg-bg-elevated text-fg font-mono">
                        {a.action}
                      </span>
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
