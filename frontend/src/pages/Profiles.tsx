import { useEffect, useMemo, useState } from 'react'
import { FileSliders, Lock, Plus } from 'lucide-react'
import { Card, SectionTitle, LevelBadge, Spinner, EmptyState } from '@/components/ui'
import { Modal } from '@/components/Modal'
import { useProfiles, useCatalog, useCreateProfile } from '@/api/hooks'
import { ApiError } from '@/api/client'
import type { CatalogEntry } from '@/types'

// Шаблоны config под типы каталога — стартовая заготовка для редактора.
function configTemplate(typeId: string): Record<string, unknown> {
  if (typeId === 'honeytoken-http') {
    const key = Array.from(crypto.getRandomValues(new Uint8Array(24)), (byte) => byte.toString(16).padStart(2, '0')).join('')
    return {
      services: [{ name: 'web', port: 8080 }],
      tokens: [
        { id: 'backup-key', kind: 'key', path: '/v1/backups', value: key },
        { id: 'backup-file', kind: 'file', path: '/backup.txt', value: 'Demo backup: no real data.' },
        { id: 'report-url', kind: 'url', path: '/reports/latest', value: 'Report ready.' },
      ],
      management: { heartbeat_interval_seconds: 5, telemetry_flush_interval_ms: 500 },
    }
  }
  if (typeId === 'redis-emulator') {
    return {
      services: [{ name: 'redis', port: 6379, password: 'S3cretTrap!' }],
      management: { heartbeat_interval_seconds: 5, telemetry_flush_interval_ms: 500 },
    }
  }
  return {
    listeners: [{ name: 'ssh', port: 22, banner: 'SSH-2.0-OpenSSH_8.9', close_after_banner: false }],
    logging: { capture_payload: true, max_payload_bytes: 4096 },
    management: { heartbeat_interval_seconds: 5, telemetry_flush_interval_ms: 500 },
  }
}

export function ProfilesPage() {
  const { data, isLoading } = useProfiles()
  const [open, setOpen] = useState(false)
  const profiles = data?.items ?? []

  return (
    <div className="p-6 space-y-5">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="h-10 w-10 rounded-xl bg-brand/15 grid place-items-center">
            <FileSliders size={18} className="text-brand" />
          </div>
          <div>
            <h1 className="text-xl font-semibold">Профили</h1>
            <p className="text-sm text-muted">Конфигурации ловушек по выбранному типу каталога</p>
          </div>
        </div>
        <button className="btn-primary" onClick={() => setOpen(true)}>
          <Plus size={16} /> Новый профиль
        </button>
      </div>

      {isLoading ? (
        <Spinner />
      ) : profiles.length === 0 ? (
        <EmptyState title="Профилей нет" hint="Создайте профиль, чтобы затем зарегистрировать ловушку" />
      ) : (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          {profiles.map((p) => (
            <Card key={p.id}>
              <SectionTitle action={<LevelBadge level={p.interaction_level} />}>{p.name}</SectionTitle>
              <div className="flex items-center gap-2 text-xs text-muted mb-3">
                <span className="font-mono">
                  {p.type_id}/{p.type_version}
                </span>
                <span>·</span>
                <span>revision {p.revision}</span>
                {p.secret_fields_set.length > 0 && (
                  <span className="chip bg-warn/10 text-warn">
                    <Lock size={10} /> {p.secret_fields_set.length} секрет(ов)
                  </span>
                )}
              </div>
              <pre className="text-xs bg-bg-elevated/60 rounded-xl p-3 font-mono text-fg overflow-auto max-h-56">
                {JSON.stringify(p.config, null, 2)}
              </pre>
            </Card>
          ))}
        </div>
      )}

      <CreateProfileModal open={open} onClose={() => setOpen(false)} />
    </div>
  )
}

function CreateProfileModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { data: catalog } = useCatalog()
  const createProfile = useCreateProfile()
  const entries = useMemo(() => catalog?.items ?? [], [catalog])

  const [typeKey, setTypeKey] = useState('')
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [configText, setConfigText] = useState('')
  const [error, setError] = useState<string | null>(null)

  const selected: CatalogEntry | undefined = useMemo(
    () => entries.find((e) => `${e.type_id}/${e.type_version}` === typeKey),
    [entries, typeKey],
  )

  useEffect(() => {
    if (open && entries.length && !typeKey) {
      const first = entries[0]
      setTypeKey(`${first.type_id}/${first.type_version}`)
    }
  }, [open, entries, typeKey])

  useEffect(() => {
    if (selected) setConfigText(JSON.stringify(configTemplate(selected.type_id), null, 2))
  }, [selected])

  async function submit() {
    setError(null)
    if (!selected || !name.trim()) {
      setError('Укажите имя и тип')
      return
    }
    let config: Record<string, unknown>
    try {
      config = JSON.parse(configText)
    } catch {
      setError('config не является корректным JSON')
      return
    }
    try {
      await createProfile.mutateAsync({
        name: name.trim(),
        description,
        type_id: selected.type_id,
        type_version: selected.type_version,
        config,
      })
      setName('')
      setDescription('')
      setTypeKey('')
      onClose()
    } catch (err) {
      if (err instanceof ApiError && (err.code === 'config_invalid' || err.code === 'validation_failed'))
        setError('config не прошёл валидацию схемы типа')
      else setError('Не удалось создать профиль')
    }
  }

  return (
    <Modal open={open} onClose={onClose} title="Новый профиль" width="max-w-2xl">
      <div className="space-y-4">
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="label">Тип ловушки</label>
            <select className="input" value={typeKey} onChange={(e) => setTypeKey(e.target.value)}>
              {entries.map((e) => (
                <option key={`${e.type_id}/${e.type_version}`} value={`${e.type_id}/${e.type_version}`}>
                  {e.title} — {e.type_id}/{e.type_version} ({e.interaction_level})
                </option>
              ))}
            </select>
          </div>
          <div>
            <label className="label">Имя профиля</label>
            <input className="input" placeholder="SSH-приманка" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
        </div>

        <div>
          <label className="label">Описание</label>
          <input className="input" value={description} onChange={(e) => setDescription(e.target.value)} />
        </div>

        <div>
          <label className="label">
            config (JSON по схеме типа{selected ? ` ${selected.type_id}/${selected.type_version}` : ''})
          </label>
          <textarea
            className="input font-mono text-xs h-56 resize-y"
            value={configText}
            onChange={(e) => setConfigText(e.target.value)}
            spellCheck={false}
          />
          <p className="text-xs text-muted mt-1">
            Секреты (например password у redis-emulator) передаются в config при создании и затем
            скрываются в ответах.
          </p>
        </div>

        {error && <p className="text-xs text-danger">{error}</p>}

        <div className="flex justify-end gap-2">
          <button className="btn-ghost" onClick={onClose}>
            Отмена
          </button>
          <button className="btn-primary" disabled={createProfile.isPending} onClick={submit}>
            {createProfile.isPending ? 'Создание…' : 'Создать профиль'}
          </button>
        </div>
      </div>
    </Modal>
  )
}
