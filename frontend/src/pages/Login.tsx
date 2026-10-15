import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { ArrowRight } from 'lucide-react'
import { api, USE_MOCK } from '@/api/client'
import { AuthShell } from '@/components/AuthShell'
import { useQueryClient } from '@tanstack/react-query'
import { replaceSessionCache } from '@/api/cache'

export function LoginPage() {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [email, setEmail] = useState(USE_MOCK ? 'admin@perimetr.ru' : '')
  const [password, setPassword] = useState(USE_MOCK ? 'demo-password-2026' : '')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setLoading(true)
    setError(null)
    try {
      const session = await api.login(email, password)
      await replaceSessionCache(qc, session)
      navigate('/app')
    } catch {
      setError('Неверный email или пароль')
    } finally {
      setLoading(false)
    }
  }

  return (
    <AuthShell title="Вход" description="Войдите, чтобы управлять ловушками и просматривать события вашей организации.">
      <form onSubmit={submit} aria-labelledby="auth-title">
        <label htmlFor="login-email" className="label">Email</label>
        <input
          id="login-email"
          className="input mb-4"
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          autoComplete="username"
        />

        <label htmlFor="login-password" className="label">Пароль</label>
        <input
          id="login-password"
          className="input mb-2"
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="current-password"
        />

        {error && <p role="alert" className="text-sm text-danger mb-2">{error}</p>}

        <button type="submit" className="btn-primary w-full mt-4" disabled={loading}>
          {loading ? 'Вход…' : 'Войти'} <ArrowRight size={16} />
        </button>

        <p className="text-sm text-muted mt-4 text-center">
          Нет аккаунта?{' '}
          <Link to="/register" className="text-brand hover:underline">
            Зарегистрироваться
          </Link>
        </p>

        {USE_MOCK && (
          <p className="text-xs text-muted mt-4 text-center">
            Демо-режим: любые данные подойдут, вход выполняется в тестовую организацию.
          </p>
        )}
      </form>
    </AuthShell>
  )
}
