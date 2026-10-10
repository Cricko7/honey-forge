import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Shield, ArrowRight } from 'lucide-react'
import { api, USE_MOCK } from '@/api/client'
import { ApiError } from '@/api/client'
import { ThemeToggle } from '@/components/ThemeToggle'

export function RegisterPage() {
  const navigate = useNavigate()
  const [email, setEmail] = useState('admin@perimetr.ru')
  const [password, setPassword] = useState('demo-password-2026')
  const [org, setOrg] = useState('ООО «Периметр»')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setLoading(true)
    setError(null)
    try {
      await api.register(email, password, org)
      navigate('/app')
    } catch (err) {
      if (err instanceof ApiError && err.code === 'email_in_use') setError('Email уже зарегистрирован')
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
          <h1 className="text-4xl font-bold leading-tight mb-4">Создайте организацию</h1>
          <p className="text-muted">
            Первый пользователь становится администратором. Он получает код вступления, чтобы
            добавлять viewer-операторов, и управляет ловушками организации.
          </p>
        </div>
        <div className="text-xs text-muted">Seg_Fault · HoneyForge API 0.1.0</div>
        <div className="absolute -right-32 -bottom-32 h-96 w-96 rounded-full bg-brand/20 blur-3xl" />
      </div>

      <div className="flex items-center justify-center p-8">
        <form onSubmit={submit} className="w-full max-w-sm">
          <h2 className="text-2xl font-semibold mb-1">Регистрация администратора</h2>
          <p className="text-sm text-muted mb-6">Создаёт организацию и первого admin-оператора.</p>

          <label className="label">Название организации</label>
          <input className="input mb-4" value={org} onChange={(e) => setOrg(e.target.value)} />

          <label className="label">Email</label>
          <input
            className="input mb-4"
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            autoComplete="username"
          />

          <label className="label">Пароль (12–128 символов)</label>
          <input
            className="input mb-2"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
          />

          {error && <p className="text-xs text-danger mb-2">{error}</p>}

          <button type="submit" className="btn-primary w-full mt-4" disabled={loading}>
            {loading ? 'Создание…' : 'Создать организацию'} <ArrowRight size={16} />
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
