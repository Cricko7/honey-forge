import { NavLink, Outlet, Navigate, useNavigate, useLocation } from 'react-router-dom'
import {
  LayoutDashboard,
  Network,
  Bug,
  ScrollText,
  Boxes,
  FileSliders,
  Shield,
  LogOut,
  Radio,
} from 'lucide-react'
import { useSession, useLiveStream } from '@/api/hooks'
import { USE_MOCK, api } from '@/api/client'
import { cn } from '@/lib/utils'
import { ThemeToggle } from '@/components/ThemeToggle'
import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { replaceSessionCache } from '@/api/cache'
import { canManage, sessionDestination } from '@/lib/auth'
import { ApiError } from '@/api/client'

const nav = [
  { to: '/app', end: true, icon: LayoutDashboard, label: 'Дашборд' },
  { to: '/app/network', icon: Network, label: 'Конструктор сети' },
  { to: '/app/traps', icon: Bug, label: 'Ловушки' },
  { to: '/app/events', icon: ScrollText, label: 'События' },
  { to: '/app/profiles', icon: FileSliders, label: 'Профили' },
  { to: '/app/catalog', icon: Boxes, label: 'Каталог типов' },
]

export function Layout() {
  const qc = useQueryClient()
  const [logoutError, setLogoutError] = useState('')
  const [loggingOut, setLoggingOut] = useState(false)
  const { data: session, isLoading, error, refetch } = useSession()
  const navigate = useNavigate()
  const location = useLocation()
  // Глобальная подписка на live-стрим: держит кэш свежим на всех экранах.
  useLiveStream()

  async function logout() {
    setLoggingOut(true)
    setLogoutError('')
    try {
      try {
        await api.logout()
      } catch (err) {
        if (!(err instanceof ApiError && err.status === 401)) throw err
      }
      await replaceSessionCache(qc)
      navigate('/login')
    } catch {
      setLogoutError('Не удалось выйти. Попробуйте ещё раз.')
    } finally {
      setLoggingOut(false)
    }
  }

  // Auth-гард: без активной сессии (401) уводим на вход вместо «сломанной» оболочки.
  if (isLoading) {
    return (
      <div className="h-screen grid place-items-center text-muted text-sm">
        <span className="h-5 w-5 rounded-full border-2 border-brand/30 border-t-brand animate-spin" />
      </div>
    )
  }
  if (sessionDestination(session, isLoading, error) === 'guest') return <Navigate to="/login" replace />
  if (error || !session) return <div className="p-8">Не удалось проверить сессию. <button className="btn-primary" onClick={() => refetch()}>Повторить</button></div>

  return (
    <div className="flex h-screen overflow-hidden gap-5 p-5 bg-bg-base">
      {/* Боковое меню — отдельная плавающая карточка с отступами со всех сторон */}
      <aside className="w-60 shrink-0 card flex flex-col overflow-hidden">
        <div className="px-5 h-16 flex items-center gap-2.5">
          <div className="h-8 w-8 rounded-lg bg-brand/10 grid place-items-center">
            <Shield size={18} className="text-brand" />
          </div>
          <div className="leading-tight">
            <div className="font-semibold text-fg">HoneyForge</div>
            <div className="text-[10px] text-muted tracking-wide">deception platform</div>
          </div>
        </div>

        <nav className="flex-1 px-3 py-4 space-y-0.5">
          {[...nav, ...(canManage(session) ? [{ to: '/app/organization', icon: Shield, label: 'Приглашения' }] : [])].map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.end}
              className={({ isActive }) =>
                cn(
                  'flex items-center gap-3 px-3 py-2 rounded-lg text-sm font-medium transition-colors',
                  isActive
                    ? 'bg-brand/10 text-brand'
                    : 'text-muted hover:text-fg hover:bg-bg-hover',
                )
              }
            >
              <item.icon size={18} />
              {item.label}
            </NavLink>
          ))}
        </nav>

        <div className="p-3">
          <div className="rounded-xl bg-bg-elevated/60 border border-line/50 p-3">
            <div className="flex items-center gap-2.5 mb-3">
              <div className="h-8 w-8 rounded-full bg-brand/10 grid place-items-center text-brand text-xs font-semibold shrink-0">
                {(session?.user.email ?? '?').slice(0, 1).toUpperCase()}
              </div>
              <div className="min-w-0 leading-tight">
                <div className="text-sm text-fg truncate">{session?.user.email ?? '—'}</div>
                <div className="text-xs text-muted truncate">
                  {session?.organization.name} · {canManage(session) ? 'Администратор' : 'Пользователь · только чтение'}
                </div>
              </div>
            </div>
            {logoutError && <p className="text-xs text-danger mb-2">{logoutError}</p>}
            <button onClick={logout} disabled={loggingOut} className="btn-ghost w-full">
              <LogOut size={16} /> Выйти
            </button>
          </div>
        </div>
      </aside>

      {/* Правая колонка: закреплённый навбар-карточка + прокручиваемый контент-карточка */}
      <div className="flex-1 flex flex-col min-w-0 gap-5">
        {/* Навбар — скруглённый, закреплён на всех разделах, контент уходит под него */}
        <header className="shrink-0 card h-16 flex items-center justify-between px-6">
          <div className="flex items-center gap-2 text-sm">
            <span className="chip bg-ok/10 text-ok">
              <span className="h-1.5 w-1.5 rounded-full bg-ok" /> System active
            </span>
            {USE_MOCK && <span className="chip bg-warn/10 text-warn">demo · mock-данные</span>}
          </div>
          <div className="flex items-center gap-4">
            <div className="flex items-center gap-2 text-xs text-muted">
              <Radio size={14} className="text-brand" />
              live-поток подключён
            </div>
            <ThemeToggle />
          </div>
        </header>

        <main className="flex-1 overflow-auto card">
          <div key={location.pathname} className="h-full animate-fadeInUp">
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  )
}
