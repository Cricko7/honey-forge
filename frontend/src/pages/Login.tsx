import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Shield, ArrowRight } from 'lucide-react'
import { api, USE_MOCK } from '@/api/client'
import { ThemeToggle } from '@/components/ThemeToggle'

export function LoginPage() {
  const navigate = useNavigate()
  const [email, setEmail] = useState('admin@perimetr.ru')
  const [password, setPassword] = useState('demo-password')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setLoading(true)
    setError(null)
    try {
      await api.login(email, password)
      navigate('/app')
    } catch {
      setError('Неверный email или пароль')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen grid lg:grid-cols-2 relative">
      <div className="absolute top-5 right-5 z-20">
        <ThemeToggle />
      </div>
      {/* Левая панель — бренд */}
      <div className="hidden lg:flex flex-col justify-between p-12 bg-grid-fade relative overflow-hidden">
        <div className="flex items-center gap-3">
          <div className="h-10 w-10 rounded-2xl bg-brand/20 grid place-items-center shadow-glow">
            <Shield className="text-brand-soft" />
          </div>
          <span className="text-xl font-semibold">HoneyForge</span>
        </div>
        <div className="max-w-md">
          <h1 className="text-4xl font-bold leading-tight mb-4">
            Конструктор ловушек для вашей сети
          </h1>
          <p className="text-muted">
            Разворачивайте honeypot-приманки, стройте ложную инфраструктуру и ловите атакующего на
            каждом шаге его расследования — в реальном времени.
          </p>
          <div className="mt-8 flex gap-6 text-sm">
            <div>
              <div className="text-2xl font-bold text-brand-soft">Low · Medium</div>
              <div className="text-muted">уровни интеракции</div>
            </div>
            <div>
              <div className="text-2xl font-bold text-brand-soft">Real-time</div>
              <div className="text-muted">поток событий</div>
            </div>
          </div>
        </div>
        <div className="text-xs text-muted">Seg_Fault · HoneyForge API 0.1.0</div>
        <div className="absolute -right-32 -bottom-32 h-96 w-96 rounded-full bg-brand/20 blur-3xl" />
      </div>

      {/* Правая панель — форма */}
      <div className="flex items-center justify-center p-8">
        <form onSubmit={submit} className="w-full max-w-sm">
          <h2 className="text-2xl font-semibold mb-1">Вход для оператора</h2>
          <p className="text-sm text-muted mb-6">
            Доступ к панели управления ловушками организации.
          </p>

          <label className="label">Email</label>
          <input
            className="input mb-4"
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            autoComplete="username"
          />

          <label className="label">Пароль</label>
          <input
            className="input mb-2"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
          />

          {error && <p className="text-xs text-danger mb-2">{error}</p>}

          <button type="submit" className="btn-primary w-full mt-4" disabled={loading}>
            {loading ? 'Вход…' : 'Войти'} <ArrowRight size={16} />
          </button>

          <p className="text-sm text-muted mt-4 text-center">
            Нет организации?{' '}
            <Link to="/register" className="text-brand hover:underline">
              Создать
            </Link>
          </p>

          {USE_MOCK && (
            <p className="text-xs text-muted mt-4 text-center">
              Демо-режим: любые данные подойдут, вход выполняется в тестовую организацию.
            </p>
          )}
        </form>
      </div>
    </div>
  )
}
