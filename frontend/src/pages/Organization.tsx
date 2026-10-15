import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Navigate } from 'react-router-dom'
import { Copy, Check, RefreshCw } from 'lucide-react'
import { api, ApiError } from '@/api/client'
import { useCanManage } from '@/api/hooks'
import { Spinner } from '@/components/ui'
import { APIKeys } from './organization/APIKeys'

const steps = [
  { title: 'Поделитесь кодом', text: 'Передайте код приглашения коллеге любым надёжным каналом.' },
  { title: 'Он регистрируется', text: 'При регистрации коллега указывает свой email, пароль и этот код.' },
  { title: 'Доступ «только чтение»', text: 'Наблюдатель видит данные организации, но не может ничего менять.' },
]

export function OrganizationPage() {
  const canManage = useCanManage()
  const qc = useQueryClient()
  const [copied, setCopied] = useState(false)
  const [copyError, setCopyError] = useState('')
  const key = ['organization', 'join-code']
  const code = useQuery({ queryKey: key, queryFn: api.getJoinCode, enabled: canManage })
  const rotation = useMutation({
    mutationFn: api.rotateJoinCode,
    meta: { alertSuccess: 'Код приглашения обновлён', alertError: 'Не удалось обновить код приглашения' },
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
      setTimeout(() => setCopied(false), 2000)
    } catch {
      setCopyError('Не удалось скопировать. Выделите код и скопируйте вручную.')
    }
  }

  return (
    <div className="page-content space-y-6">
      <header>
        <h1 className="text-2xl font-semibold tracking-tight text-fg">Доступ к организации</h1>
        <p className="text-sm text-muted mt-2 max-w-xl leading-relaxed">
          Выдайте код доступа коллеге. Он зарегистрируется с этим кодом и получит роль
          наблюдателя — только просмотр данных организации, без управления ловушками.
        </p>
      </header>
      <section className="border-t border-line pt-5">
        <div className="flex items-center gap-2.5 mb-4">
          <div className="leading-tight">
            <h2 className="text-base font-semibold text-fg">Код приглашения</h2>
            <div className="text-xs text-muted">Один активный код на организацию</div>
          </div>
        </div>

        {code.isLoading && (
          <div className="py-8 grid place-items-center">
            <Spinner />
          </div>
        )}

        {code.error && (
          <div role="alert" className="relative">
            <p className="text-danger text-sm">{code.error.message}</p>
            <button className="btn-ghost mt-3" onClick={() => code.refetch()}>
              <RefreshCw size={15} /> Повторить
            </button>
          </div>
        )}

        {code.data && (
          <div className="relative space-y-4">
            <div className="flex flex-col sm:flex-row gap-3 sm:items-stretch">
              <div className="flex-1 flex items-center rounded-md border border-line bg-bg-elevated px-4 py-3 font-mono text-lg tracking-wider text-fg select-all overflow-x-auto">
                {code.data.code}
              </div>
              <button
                onClick={copy}
                className={`btn shrink-0 min-w-[9.5rem] ${
                  copied ? 'bg-ok/10 text-ok border border-ok/30' : 'btn-primary'
                }`}
              >
                {copied ? (
                  <>
                    <Check size={16} /> Скопировано
                  </>
                ) : (
                  <>
                    <Copy size={16} /> Скопировать
                  </>
                )}
              </button>
            </div>

            <div className="flex flex-wrap items-center gap-3 pt-1">
              <button
                className="btn-ghost"
                disabled={rotation.isPending || code.isFetching}
                onClick={() => rotation.mutate(code.data!.revision)}
              >
                <RefreshCw size={15} className={rotation.isPending ? 'animate-spin' : ''} />
                {rotation.isPending ? 'Перевыпуск…' : 'Перевыпустить код'}
              </button>
              <p className="text-xs text-muted max-w-sm">
                После перевыпуска прежний код перестанет работать. Уже зарегистрированные
                пользователи сохранят доступ.
              </p>
            </div>
          </div>
        )}

        {copyError && (
          <p role="alert" className="relative text-danger text-sm mt-3">
            {copyError}
          </p>
        )}
        {rotation.error && (
          <p role="alert" className="relative text-danger text-sm mt-3">
            {rotation.error instanceof ApiError && rotation.error.code === 'join_code_changed'
              ? 'Код уже изменился. Загружен актуальный код — повторите при необходимости.'
              : 'Не удалось перевыпустить код. Попробуйте ещё раз.'}
          </p>
        )}
      </section>

      <APIKeys />
      <details className="border-t border-line pt-4">
        <summary className="min-h-11 cursor-pointer py-2 text-sm text-brand">Как пригласить коллегу</summary>
        <ol className="list-decimal pl-5 space-y-3 text-sm">
          {steps.map(step => <li key={step.title}>
            <span className="font-medium">{step.title}. </span>
            <span className="text-muted">{step.text}</span>
          </li>)}
        </ol>
      </details>
    </div>
  )
}
