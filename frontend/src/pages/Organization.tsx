import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Navigate } from 'react-router-dom'
import { api, ApiError } from '@/api/client'
import { useCanManage } from '@/api/hooks'
import { Card, Spinner } from '@/components/ui'

export function OrganizationPage() {
  const canManage = useCanManage()
  const qc = useQueryClient()
  const [copied, setCopied] = useState(false)
  const [copyError, setCopyError] = useState('')
  const key = ['organization', 'join-code']
  const code = useQuery({ queryKey: key, queryFn: api.getJoinCode, enabled: canManage })
  const rotation = useMutation({
    mutationFn: api.rotateJoinCode,
    onSuccess: (next) => {
      qc.setQueryData(key, next)
      setCopied(false)
      setCopyError('')
    },
    onError: (err) => {
      if (err instanceof ApiError && err.code === 'join_code_changed') qc.invalidateQueries({ queryKey: key })
    },
  })

  if (!canManage) return <Navigate to="/app" replace />

  async function copy() {
    try {
      await navigator.clipboard.writeText(code.data!.code)
      setCopied(true)
      setCopyError('')
    } catch {
      setCopyError('Не удалось скопировать. Выделите код и скопируйте вручную.')
    }
  }

  return (
    <div className="p-6 space-y-5">
      <h1 className="text-xl font-semibold">Приглашения пользователей</h1>
      <Card>
        <p className="text-sm text-muted mb-4">
          Передайте код пользователю. При регистрации он укажет свой email, пароль и этот код.
          Пользователь сможет только смотреть и читать данные организации.
        </p>
        {code.isLoading && <Spinner />}
        {code.error && <div role="alert">
          <p className="text-danger">{code.error.message}</p>
          <button className="btn-ghost mt-2" onClick={() => code.refetch()}>Повторить</button>
        </div>}
        {code.data && <>
          <label className="label" htmlFor="join-code">Код приглашения</label>
          <input id="join-code" className="input font-mono mb-4" readOnly value={code.data.code} />
          <div className="flex gap-3">
            <button className="btn-ghost" onClick={copy}>{copied ? 'Скопировано' : 'Скопировать'}</button>
            <button className="btn-primary" disabled={rotation.isPending || code.isFetching}
              onClick={() => rotation.mutate(code.data!.revision)}>
              {rotation.isPending ? 'Перевыпуск…' : 'Перевыпустить код'}
            </button>
          </div>
          <p className="text-sm text-muted mt-4">
            После перевыпуска прежний код перестанет работать. Уже зарегистрированные пользователи сохранят доступ.
          </p>
        </>}
        {copyError && <p role="alert" className="text-danger mt-3">{copyError}</p>}
        {rotation.error && <p role="alert" className="text-danger mt-3">
          {rotation.error instanceof ApiError && rotation.error.code === 'join_code_changed'
            ? 'Код уже изменился. Загружен актуальный код — повторите при необходимости.'
            : 'Не удалось перевыпустить код. Попробуйте ещё раз.'}
        </p>}
      </Card>
    </div>
  )
}
