import { NavLink, Outlet, Navigate, useNavigate, useLocation } from 'react-router-dom'
import {
  LayoutDashboard,
  Network,
  Server,
  Bug,
  ScrollText,
  Boxes,
  FileSliders,
  Link2,
  LogOut,
} from 'lucide-react'
import { useSession, useLiveStream } from '@/api/hooks'
import { USE_MOCK, api } from '@/api/client'
import { cn } from '@/lib/utils'
import { ThemeToggle } from '@/components/ThemeToggle'
import { Logo } from '@/components/Logo'
import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { replaceSessionCache } from '@/api/cache'
import { canManage, sessionDestination } from '@/lib/auth'
import { ApiError } from '@/api/client'

const nav = [
  { to: '/app', end: true, icon: LayoutDashboard, label: 'Дашборд' },
  { to: '/app/network', icon: Network, label: 'Конструктор сети' },
  { to: '/app/servers', icon: Server, label: 'Серверы' },
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

  // Активный раздел для контекстной шапки: самое длинное совпадение пути.
  const sections = canManage(session)
    ? [...nav, { to: '/app/organization', end: false, icon: Link2, label: 'Приглашения' }]
    : nav
  const active =
    sections
      .filter((s) =>
        s.end ? location.pathname === s.to : location.pathname === s.to || location.pathname.startsWith(s.to + '/'),
      )
      .sort((a, b) => b.to.length - a.to.length)[0] ?? nav[0]
  const ActiveIcon = active.icon

  return (
    <div className="flex h-screen overflow-hidden gap-5 p-5 bg-bg-base">
      {/* Боковое меню — отдельная плавающая карточка с отступами со всех сторон */}
      <aside className="w-60 shrink-0 card flex flex-col overflow-hidden">
        <div className="px-5 h-16 flex items-center gap-2.5">
          <Logo size={30} className="shrink-0" />
          <div className="leading-tight">
            <div className="font-semibold text-fg">HoneyForge</div>
            <div className="text-[10px] text-muted tracking-wide">deception platform</div>
          </div>
        </div>

        <nav className="flex-1 px-3 py-4 space-y-0.5">
          {[...nav, ...(canManage(session) ? [{ to: '/app/organization', icon: Link2, label: 'Приглашения' }] : [])].map((item) => (
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
        <header className="shrink-0 card h-16 flex items-center justify-between pl-4 pr-3.5">
          {/* Левая часть — контекст текущего раздела */}
          <div className="flex items-center gap-3 min-w-0">
            <div className="h-9 w-9 rounded-xl bg-gradient-to-br from-brand/25 to-brand/5 text-brand grid place-items-center shrink-0 ring-1 ring-brand/15 shadow-sm">
              <ActiveIcon size={18} />
            </div>
            <div className="leading-tight min-w-0">
              <div className="text-sm font-semibold text-fg truncate">{active.label}</div>
              <div className="text-[11px] text-muted truncate">
                {session.organization.name} · deception platform
              </div>
            </div>
          </div>

          {/* Правая часть — живой статус и управление */}
          <div className="flex items-center gap-2">
            <span className="hidden sm:inline-flex items-center gap-2 text-xs font-medium text-muted">
              <span className="h-1.5 w-1.5 rounded-full bg-ok animate-pulseGlow" />
              Поток активен
            </span>
            <span className="sm:hidden h-1.5 w-1.5 rounded-full bg-ok animate-pulseGlow" />
            {USE_MOCK && (
              <span className="chip bg-warn/10 text-warn ring-1 ring-warn/20">demo · mock</span>
            )}
            <div className="h-5 w-px bg-line mx-1" />
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
