import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Shield, ArrowRight } from 'lucide-react'
import { api, USE_MOCK } from '@/api/client'
import { ApiError } from '@/api/client'
import { ThemeToggle } from '@/components/ThemeToggle'
import { useQueryClient } from '@tanstack/react-query'
import { replaceSessionCache } from '@/api/cache'

import type { RegistrationMode } from '@/lib/auth'

export function RegisterPage() {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [email, setEmail] = useState('admin@perimetr.ru')
  const [password, setPassword] = useState('demo-password-2026')
  const [mode, setMode] = useState<RegistrationMode>('join')
  const [code, setCode] = useState('')
  const [org, setOrg] = useState('ООО «Периметр»')
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
    <div className="min-h-screen grid lg:grid-cols-2 relative">
      <div className="absolute top-5 right-5 z-20">
        <ThemeToggle />
      </div>

      <div className="hidden lg:flex flex-col justify-between p-12 bg-grid-fade relative overflow-hidden">
        <div className="flex items-center gap-3">
          <div className="h-10 w-10 rounded-2xl bg-brand/20 grid place-items-center shadow-glow">
            <Shield className="text-brand" />
          </div>
          <span className="text-xl font-semibold">HoneyForge</span>
        </div>
        <div className="max-w-md">
          <h1 className="text-4xl font-bold leading-tight mb-4">Присоединитесь к организации</h1>
          <p className="text-muted">
            Первый пользователь становится администратором. Он получает код вступления, чтобы
            добавлять пользователей с доступом только для чтения, и управляет ловушками организации.
          </p>
        </div>
        <div className="text-xs text-muted">Seg_Fault · HoneyForge API 0.1.0</div>
        <div className="absolute -right-32 -bottom-32 h-96 w-96 rounded-full bg-brand/20 blur-3xl" />
      </div>

      <div className="flex items-center justify-center p-8">
        <form onSubmit={submit} className="w-full max-w-sm">
          <h2 className="text-2xl font-semibold mb-1">Регистрация</h2>
          <p className="text-sm text-muted mb-6">По коду администратора — доступ к просмотру. Новая организация — права администратора.</p>

          <label className="label">Способ регистрации</label>
          <select className="input mb-4" value={mode} onChange={(e) => setMode(e.target.value as RegistrationMode)}>
            <option value="join">Пользователь — по коду приглашения</option>
            <option value="create">Администратор — новая организация</option>
          </select>
          {mode === 'join' ? <>
            <label className="label">Код приглашения</label>
            <input className="input mb-4" required value={code} onChange={(e) => setCode(e.target.value)} />
          </> : <>
            <label className="label">Название организации</label>
            <input className="input mb-4" required maxLength={100} value={org} onChange={(e) => setOrg(e.target.value)} />
          </>}

          <label className="label">Email</label>
          <input
            className="input mb-4"
            type="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            autoComplete="username"
          />

          <label className="label">Пароль (12–128 символов)</label>
          <input
            className="input mb-2"
            type="password"
            required minLength={12} maxLength={128}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
          />

          {error && <p className="text-xs text-danger mb-2">{error}</p>}

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
      </div>
    </div>
  )
}
