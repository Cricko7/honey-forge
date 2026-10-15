import { Select } from '@/components/Select'
import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { ArrowRight } from 'lucide-react'
import { api, USE_MOCK } from '@/api/client'
import { ApiError } from '@/api/client'
import { AuthShell } from '@/components/AuthShell'
import { useQueryClient } from '@tanstack/react-query'
import { replaceSessionCache } from '@/api/cache'

import type { RegistrationMode } from '@/lib/auth'

export function RegisterPage() {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [email, setEmail] = useState(USE_MOCK ? 'admin@perimetr.ru' : '')
  const [password, setPassword] = useState(USE_MOCK ? 'demo-password-2026' : '')
  const [mode, setMode] = useState<RegistrationMode>('join')
  const [code, setCode] = useState('')
  const [org, setOrg] = useState(USE_MOCK ? 'ООО «Периметр»' : '')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setLoading(true)
    setError(null)
    try {
      const session = await api.register(email, password, mode === 'join' ? code : org, mode)
      await replaceSessionCache(qc, session)
      navigate('/app')
    } catch (err) {
      if (err instanceof ApiError && err.code === 'email_in_use') setError('Email уже зарегистрирован')
      else if (err instanceof ApiError && err.code === 'invalid_join_code') setError('Код недействителен. Запросите актуальный код у администратора.')
      else if (err instanceof ApiError && err.code === 'demo_unavailable') setError(err.message)
      else if (err instanceof ApiError && err.code === 'validation_failed')
        setError('Проверьте поля: пароль 12–128 символов, корректный email, название 1–100 символов')
      else setError('Не удалось зарегистрироваться')
    } finally {
      setLoading(false)
    }
  }

  return (
    <AuthShell title="Создать аккаунт" description="Код приглашения даёт доступ к просмотру. При создании новой организации вы станете её администратором.">
      <form onSubmit={submit} aria-labelledby="auth-title">
        <label className="label">Способ регистрации</label>
        <Select label="Способ регистрации" className="mb-4" value={mode} onChange={(value) => setMode(value as RegistrationMode)}
          options={[{ value: 'join', label: 'Пользователь — по коду приглашения' }, { value: 'create', label: 'Администратор — новая организация' }]} />
        {mode === 'join' ? <>
          <label htmlFor="register-code" className="label">Код приглашения</label>
          <input id="register-code" className="input mb-4" required value={code} onChange={(e) => setCode(e.target.value)} />
        </> : <>
          <label htmlFor="register-organization" className="label">Название организации</label>
          <input id="register-organization" className="input mb-4" required maxLength={100} value={org} onChange={(e) => setOrg(e.target.value)} />
        </>}

        <label htmlFor="register-email" className="label">Email</label>
        <input
          id="register-email"
          className="input mb-4"
          type="email"
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          autoComplete="username"
        />

        <label htmlFor="register-password" className="label">Пароль (12–128 символов)</label>
        <input
          id="register-password"
          className="input mb-2"
          type="password"
          required minLength={12} maxLength={128}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="new-password"
        />

        {error && <p role="alert" className="text-sm text-danger mb-2">{error}</p>}

        <button type="submit" className="btn-primary w-full mt-4" disabled={loading}>
          {loading ? 'Регистрация…' : mode === 'join' ? 'Зарегистрироваться по коду' : 'Создать организацию'} <ArrowRight size={16} />
        </button>

        <p className="text-sm text-muted mt-4 text-center">
          Уже есть аккаунт?{' '}
          <Link to="/login" className="text-brand hover:underline">
            Войти
          </Link>
        </p>

        {USE_MOCK && (
          <p className="text-xs text-muted mt-3 text-center">
            Демо-режим: регистрация имитируется, вход выполняется в тестовую организацию.
          </p>
        )}
      </form>
    </AuthShell>
  )
}
