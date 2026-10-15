import { useState } from 'react'
import { Lock, Plus } from 'lucide-react'
import { Card, SectionTitle, LevelBadge, Spinner, EmptyState, LoadError } from '@/components/ui'
import { useProfiles, useCanManage } from '@/api/hooks'
import { configPorts } from '@/lib/profile-config'
import { CreateProfileModal } from './profiles/CreateProfileModal'

export function ProfilesPage() {
  const canManage = useCanManage()
  const { data, isLoading, error, refetch } = useProfiles()
  const [open, setOpen] = useState(false)
  const profiles = data?.items ?? []

  return <div className="page-content space-y-5">
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Профили</h1>
        <p className="text-sm text-muted">Сохранённые настройки поведения ловушек.</p>
      </div>
      {canManage && <button className="btn-primary" onClick={() => setOpen(true)}><Plus size={16} /> Новый профиль</button>}
    </div>
    {error && <LoadError message={data ? 'Не удалось обновить профили. Показаны последние загруженные данные.' : 'Не удалось загрузить профили. Проверьте подключение.'} onRetry={() => { void refetch() }} />}
    {isLoading ? <Spinner /> : !data && error ? null : profiles.length === 0 ?
      <EmptyState title="Профилей нет" hint={canManage ? 'Создайте профиль или добавьте ловушку — профиль будет создан автоматически.' : 'Профиль может создать администратор организации.'} /> :
      <div>
        {profiles.map(profile => <Card key={profile.id}>
          <SectionTitle action={<LevelBadge level={profile.interaction_level} />}>{profile.name}</SectionTitle>
          <div className="flex flex-wrap items-center gap-2 text-xs text-muted mb-3">
            <span className="font-mono">{profile.type_id}/{profile.type_version}</span>
            <span>· Версия {profile.revision}</span>
            {profile.secret_fields_set.length > 0 && <span className="chip bg-warn/10 text-warn"><Lock size={12} /> Секреты скрыты</span>}
          </div>
          {profile.description && <p className="mb-3 text-sm">{profile.description}</p>}
          <p className="text-sm"><span className="text-muted">Порты: </span>{configPorts(profile.config).join(', ') || 'Не заданы'}</p>
          <details className="mt-3">
            <summary className="min-h-11 py-3 cursor-pointer text-sm text-brand">Конфигурация JSON</summary>
            <pre className="text-xs bg-bg-elevated/60 rounded-lg p-3 font-mono overflow-auto max-h-56">{JSON.stringify(profile.config, null, 2)}</pre>
          </details>
        </Card>)}
      </div>}
    <CreateProfileModal open={canManage && open} onClose={() => setOpen(false)} />
  </div>
}
