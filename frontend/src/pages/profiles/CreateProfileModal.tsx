import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { Select } from '@/components/Select'
import { Modal } from '@/components/Modal'
import { LoadError, Spinner } from '@/components/ui'
import { useCatalog, useCreateProfile } from '@/api/hooks'
import { ApiError } from '@/api/client'
import { actionErrorMessage } from '@/lib/alerts'
import { parseProfileConfig } from '@/lib/profile-config'
import { initialProfileConfig, profileConfigIssues } from '@/lib/profile-editor'
import { ProfileConfigEditor } from './ProfileConfigEditor'

export function CreateProfileModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { data: catalog, isLoading, error: catalogError, refetch } = useCatalog()
  const createProfile = useCreateProfile()
  const entries = useMemo(() => (catalog?.items ?? []).filter(entry => entry.available_for_new_profiles), [catalog])
  const [typeKey, setTypeKey] = useState('')
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const [error, setError] = useState('')
  const selected = entries.find(entry => `${entry.type_id}/${entry.type_version}` === typeKey)
  const configText = drafts[typeKey] ?? ''

  useEffect(() => {
    if (open && entries.length && !typeKey) setTypeKey(`${entries[0].type_id}/${entries[0].type_version}`)
  }, [open, entries, typeKey])

  useEffect(() => {
    if (selected && drafts[typeKey] === undefined) {
      const initial = JSON.stringify(initialProfileConfig(selected.type_id), null, 2)
      setDrafts(previous => ({ ...previous, [typeKey]: initial }))
    }
  }, [selected, drafts, typeKey])

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    if (createProfile.isPending) return
    setError('')
    if (!selected || !name.trim()) { setError('Укажите имя профиля и выберите тип ловушки.'); return }
    let config: Record<string, unknown>
    try { config = parseProfileConfig(configText) } catch (error) { setError((error as Error).message); return }
    const issues = profileConfigIssues(config, selected.type_id)
    if (Object.keys(issues).length) { setError(Object.values(issues).join(' ')); return }
    try {
      await createProfile.mutateAsync({ name: name.trim(), description, type_id: selected.type_id, type_version: selected.type_version, config })
      setName(''); setDescription(''); setTypeKey(''); setDrafts({})
      onClose()
    } catch (error) {
      setError(error instanceof ApiError && (error.code === 'config_invalid' || error.code === 'validation_failed')
        ? 'Настройки не соответствуют выбранному типу. Проверьте порт и обязательные параметры в расширенных настройках. Введённые данные сохранены.'
        : actionErrorMessage(error))
    }
  }

  return <Modal open={open} onClose={onClose} title="Новый профиль" width="max-w-2xl">
    <form onSubmit={submit} className="space-y-4">
      <p className="text-sm text-muted">Профиль задаёт поведение ловушки. При смене типа и закрытии окна черновик сохраняется до ухода со страницы.</p>
      {catalogError && <LoadError message="Не удалось загрузить типы ловушек." onRetry={() => { void refetch() }} />}
      {isLoading ? <Spinner /> : <fieldset disabled={createProfile.isPending} className="space-y-4">
        <label className="label">Тип ловушки
          <Select label="Тип ловушки" value={typeKey} onChange={value => { setTypeKey(value); setError('') }}
            options={entries.map(entry => ({ value: `${entry.type_id}/${entry.type_version}`, label: `${entry.title} · версия ${entry.type_version}` }))} />
        </label>
        <label className="label" htmlFor="profile-name">Имя профиля
          <input id="profile-name" className="input mt-1" required placeholder="SSH-приманка" value={name} onChange={event => setName(event.target.value)} />
        </label>
        <label className="label" htmlFor="profile-description">Описание · необязательно
          <input id="profile-description" className="input mt-1" value={description} onChange={event => setDescription(event.target.value)} />
        </label>
        {selected?.type_id === 'custom-http' && <p className="text-sm text-muted">HTTP-поведение удобнее собрать из блоков: <Link to="/app/constructor" className="text-brand" onClick={onClose}>открыть конструктор</Link>.</p>}
        {selected && configText && <ProfileConfigEditor typeId={selected.type_id} value={configText} onChange={value => { setDrafts(previous => ({ ...previous, [typeKey]: value })); setError('') }} />}
      </fieldset>}
      {error && <p role="alert" className="text-sm text-danger">{error}</p>}
      <div className="flex flex-wrap justify-end gap-2 border-t border-line pt-4">
        <button type="button" className="btn-ghost" onClick={onClose}>Закрыть</button>
        <button type="submit" className="btn-primary" disabled={createProfile.isPending || !selected}>
          {createProfile.isPending ? 'Создание…' : 'Создать профиль'}
        </button>
      </div>
    </form>
  </Modal>
}
