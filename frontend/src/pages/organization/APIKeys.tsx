import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { BookOpen, Copy, KeyRound, Trash2 } from 'lucide-react'
import { api, USE_MOCK } from '@/api/client'
import { Spinner } from '@/components/ui'
import { APIKeyGuide } from './APIKeyGuide'

const queryKey = ['organization', 'api-keys']

export function APIKeys() {
  const qc = useQueryClient()
  const [name, setName] = useState('')
  const [guideOpen, setGuideOpen] = useState(false)
  const [secret, setSecret] = useState<{ id: string; token: string } | null>(null)
  const [copyStatus, setCopyStatus] = useState('')
  const keys = useQuery({ queryKey, queryFn: api.listAPIKeys, enabled: !USE_MOCK })
  const create = useMutation({
    mutationFn: api.createAPIKey,
    onSuccess: (key) => {
      setSecret({ id: key.id, token: key.token })
      setName('')
      setCopyStatus('')
      qc.invalidateQueries({ queryKey })
    },
  })
  const revoke = useMutation({
    mutationFn: api.revokeAPIKey,
    onSuccess: (_, id) => {
      if (secret?.id === id) {
        setSecret(null)
        create.reset()
      }
      qc.invalidateQueries({ queryKey })
    },
  })

  async function copy() {
    if (!secret) return

    try {
      await navigator.clipboard.writeText(secret.token)
      setCopyStatus('Скопировано')
    } catch {
      setCopyStatus('Не удалось скопировать. Выделите ключ и скопируйте вручную.')
    }
  }

  return (
    <section className="card p-6 lg:p-7 space-y-5">
      <div className="flex items-center gap-2.5 flex-wrap">
        <KeyRound size={18} className="text-brand" />
        <h2 className="font-semibold">API-ключи</h2>
        <button type="button" className="btn-ghost sm:ml-auto" onClick={() => setGuideOpen(true)}>
          <BookOpen size={16} /> Документация API
        </button>
      </div>
      <p className="text-sm text-muted">
        Подключите свою интеграцию: данные организации, события в реальном времени,
        создание и запуск ловушек. Ключ действует с текущими правами владельца до отзыва.
        Выпуск и отзыв доступны администраторам через вход в приложение.
      </p>
      {USE_MOCK ? (
        <p className="text-sm text-muted">Выпуск ключей доступен с подключённым сервером.</p>
      ) : (
        <>
          <form className="flex flex-wrap gap-3" onSubmit={(e) => {
            e.preventDefault()
            create.mutate(name.trim())
          }}>
            <input
              aria-label="Название API-ключа"
              className="input flex-1 min-w-48"
              placeholder="Название интеграции"
              maxLength={120}
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
            <button className="btn btn-primary" disabled={create.isPending || !name.trim() || !!secret}>
              {create.isPending ? 'Выпуск…' : 'Выпустить ключ'}
            </button>
          </form>
          {secret && (
            <div className="rounded-xl border border-brand/30 bg-brand/5 p-4 space-y-3">
              <p className="text-sm">Сохраните ключ сейчас: повторно его посмотреть нельзя.</p>
              <code className="block break-all select-all text-sm">{secret.token}</code>
              <div className="flex flex-wrap gap-3">
                <button className="btn-ghost" onClick={copy}><Copy size={15} /> Скопировать</button>
                <button className="btn-ghost" onClick={() => {
                  setSecret(null)
                  create.reset()
                  setCopyStatus('')
                }}>Сохранён, скрыть</button>
              </div>
              {copyStatus && <p role="status" className="text-xs text-muted">{copyStatus}</p>}
            </div>
          )}
          {keys.isLoading && <Spinner />}
          {keys.error && (
            <div role="alert" className="text-sm text-danger">
              {keys.error.message}
              <button className="btn-ghost ml-2" onClick={() => keys.refetch()}>Повторить</button>
            </div>
          )}
          {keys.data?.items.length === 0 && <p className="text-sm text-muted">Активных ключей пока нет.</p>}
          <ul className="divide-y divide-line">
            {keys.data?.items.map((key) => (
              <li key={key.id} className="py-3 flex items-center justify-between gap-3">
                <div className="min-w-0">
                  <p className="text-sm font-medium break-words">{key.name}</p>
                  <p className="text-xs text-muted">{key.prefix}… · {new Date(key.created_at).toLocaleDateString('ru-RU')}</p>
                </div>
                <button className="btn-ghost text-danger" disabled={revoke.isPending} onClick={() => revoke.mutate(key.id)}>
                  <Trash2 size={15} /> Отозвать
                </button>
              </li>
            ))}
          </ul>
          {(create.error || revoke.error) && <p role="alert" className="text-sm text-danger">{(create.error || revoke.error)?.message}</p>}
        </>
      )}
      <APIKeyGuide open={guideOpen} onClose={() => setGuideOpen(false)} />
    </section>
  )
}
