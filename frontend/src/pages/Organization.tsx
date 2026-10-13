import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Navigate } from 'react-router-dom'
import { Users, KeyRound, Copy, Check, RefreshCw, Share2, UserPlus, Eye } from 'lucide-react'
import { api, ApiError } from '@/api/client'
import { useCanManage } from '@/api/hooks'
import { Spinner } from '@/components/ui'
import { APIKeys } from './organization/APIKeys'

const steps = [
  { icon: Share2, title: 'Поделитесь кодом', text: 'Передайте код приглашения коллеге любым надёжным каналом.' },
  { icon: UserPlus, title: 'Он регистрируется', text: 'При регистрации коллега указывает свой email, пароль и этот код.' },
  { icon: Eye, title: 'Доступ «только чтение»', text: 'Наблюдатель видит данные организации, но не может ничего менять.' },
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
    <div className="p-6 lg:p-8 max-w-4xl mx-auto space-y-6">
      {/* Заголовок */}
      <header className="animate-fadeInUp">
        <span className="chip bg-brand/10 text-brand mb-3">
          <Users size={13} /> Управление доступом
        </span>
        <h1 className="text-2xl font-semibold tracking-tight text-fg">Доступ к организации</h1>
        <p className="text-sm text-muted mt-2 max-w-xl leading-relaxed">
          Выдайте код доступа коллеге. Он зарегистрируется с этим кодом и получит роль
          наблюдателя — только просмотр данных организации, без управления ловушками.
        </p>
      </header>

      {/* Карточка кода приглашения */}
      <section
        className="card p-6 lg:p-7 relative overflow-hidden animate-fadeInUp"
        style={{ animationDelay: '70ms' }}
      >
        {/* фоновое свечение в фирменном фиолете */}
        <div className="pointer-events-none absolute -top-24 -right-16 h-64 w-64 rounded-full bg-brand/10 blur-3xl" />

        <div className="relative flex items-center gap-2.5 mb-5">
          <div className="h-9 w-9 rounded-xl bg-brand/10 grid place-items-center text-brand shrink-0">
            <KeyRound size={18} />
          </div>
          <div className="leading-tight">
            <div className="text-sm font-semibold text-fg">Код приглашения</div>
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
            {/* поле с кодом + копирование */}
            <div className="flex flex-col sm:flex-row gap-3 sm:items-stretch">
              <div className="flex-1 flex items-center rounded-xl border border-line bg-bg-elevated px-4 py-3.5 font-mono text-lg sm:text-xl tracking-[0.2em] text-fg select-all overflow-x-auto">
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

      {/* Как это работает */}
      <section className="grid sm:grid-cols-3 gap-4">
        {steps.map((step, i) => (
          <div
            key={step.title}
            className="card card-hover p-5 animate-fadeInUp"
            style={{ animationDelay: `${140 + i * 70}ms` }}
          >
            <div className="flex items-center gap-2.5 mb-3">
              <div className="h-8 w-8 rounded-lg bg-brand/10 grid place-items-center text-brand shrink-0">
                <step.icon size={16} />
              </div>
              <span className="text-xs font-semibold text-muted tabular-nums">Шаг {i + 1}</span>
            </div>
            <div className="text-sm font-medium text-fg mb-1">{step.title}</div>
            <p className="text-xs text-muted leading-relaxed">{step.text}</p>
          </div>
        ))}
      </section>
    </div>
  )
}
