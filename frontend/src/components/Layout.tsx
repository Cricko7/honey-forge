import { NavLink, Outlet, Navigate, useNavigate, useLocation } from 'react-router-dom'
import {
  LayoutDashboard,
  Server,
  Bug,
  ScrollText,
  Boxes,
  Workflow,
  Network,
  FileSliders,
  Link2,
  LogOut,
  ShieldCheck,
} from 'lucide-react'
import { useSession, useLiveStream } from '@/api/hooks'
import { USE_MOCK, api } from '@/api/client'
import { cn } from '@/lib/utils'
import { ThemeToggle } from '@/components/ThemeToggle'
import { Logo } from '@/components/Logo'
import { useState } from 'react'
import { replaceSessionCache } from '@/api/cache'
import { canManage, canManageTraps, sessionDestination } from '@/lib/auth'
import { usePresence } from '@/lib/presence'
import { ApiError } from '@/api/client'
import { AlertsProvider, AlertsBell, useAlerts } from '@/components/Alerts'
import type { SessionView } from '@/types'
import { SessionScope, useSessionClient } from '@/api/session-scope'

const nav = [
  { to: '/app', end: true, icon: LayoutDashboard, label: 'Обзор' },
  { to: '/app/constructor', icon: Workflow, label: 'Конструктор ловушек' },
  { to: '/app/network', icon: Network, label: 'Конструктор сети' },
  { to: '/app/servers', icon: Server, label: 'Серверы' },
  { to: '/app/traps', icon: Bug, label: 'Ловушки' },
  { to: '/app/events', icon: ScrollText, label: 'События' },
  { to: '/app/profiles', icon: FileSliders, label: 'Профили' },
  { to: '/app/catalog', icon: Boxes, label: 'Каталог типов' },
]

export function Layout() {
  const { data: session, isLoading, error, refetch } = useSession()
  const sessionClient = useSessionClient()
  if (isLoading) return <div className="h-screen grid place-items-center text-muted text-sm"><span className="h-5 w-5 rounded-full border-2 border-brand/30 border-t-brand animate-spin" /></div>
  if (sessionDestination(session, isLoading, error) === 'guest') return <Navigate to="/login" replace />
  const unavailable = <div className="p-8">Не удалось проверить сессию. <button className="btn-primary" onClick={() => refetch()}>Повторить</button></div>
  if (!session) return unavailable
  return <SessionScope key={`${session.organization.id}:${session.user.id}:${session.csrf_token}`} sessionClient={sessionClient}>
    {error ? unavailable : <AlertsProvider><AuthenticatedLayout session={session} /></AlertsProvider>}
  </SessionScope>
}

function AuthenticatedLayout({ session }: { session: SessionView }) {
  usePresence(session.user.id)
  const qc = useSessionClient()
  const [logoutError, setLogoutError] = useState('')
  const [loggingOut, setLoggingOut] = useState(false)
  const { onStream, notify } = useAlerts()
  const navigate = useNavigate()
  const location = useLocation()
  useLiveStream(undefined, onStream)

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
      notify({ id: crypto.randomUUID(), title: 'Не удалось выйти', message: 'Попробуйте ещё раз.', level: 'error' })
    } finally {
      setLoggingOut(false)
    }
  }

  const sections = canManage(session)
    ? [...nav, { to: '/app/admin', end: false, icon: ShieldCheck, label: 'Админ-панель' }, { to: '/app/organization', end: false, icon: Link2, label: 'Приглашения' }]
    : nav
  return (
    <div className="flex h-dvh flex-col md:flex-row overflow-hidden bg-bg-base">
      <a href="#main-content" className="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-[120] focus:bg-bg-panel focus:p-3">К содержимому</a>
      <aside className="md:w-56 shrink-0 bg-bg-panel border-b md:border-b-0 md:border-r border-line flex flex-col min-h-0">
        <div className="px-5 h-12 md:h-16 flex items-center gap-2.5">
          <Logo size={30} className="shrink-0" />
          <div className="leading-tight">
            <div className="font-semibold text-fg">HoneyForge</div>
          </div>
        </div>

        <nav aria-label="Основная навигация" className="flex md:block md:flex-1 gap-1 overflow-x-auto md:overflow-y-auto px-3 py-1 md:py-2 md:space-y-0.5">
          {sections.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.end}
              className={({ isActive }) =>
                cn(
                  'flex shrink-0 items-center gap-3 px-3 py-2 rounded-md text-sm font-medium transition-colors whitespace-nowrap',
                  isActive
                    ? 'bg-brand/10 text-brand'
                    : 'text-muted hover:text-fg hover:bg-bg-hover',
                )
              }
            >
              <item.icon size={17} aria-hidden="true" />
              {item.label}
            </NavLink>
          ))}
        </nav>

        <div className="px-3 py-2 md:py-3 border-t border-line">
          <div className="flex md:block items-center gap-3">
            <div className="min-w-0 flex-1 md:mb-3">
              <div className="min-w-0 leading-tight">
                <div className="text-sm text-fg truncate">{session?.user.email ?? '—'}</div>
                <div className="text-xs text-muted truncate">
                  {canManage(session) ? 'Администратор' : canManageTraps(session) ? 'Оператор ловушек' : 'Только чтение'}
                </div>
              </div>
            </div>
            {logoutError && <p className="text-xs text-danger mb-2">{logoutError}</p>}
            <button type="button" onClick={logout} disabled={loggingOut} className="btn-ghost md:w-full">
              <LogOut size={16} /> Выйти
            </button>
          </div>
        </div>
      </aside>

      <div className="flex-1 flex flex-col min-w-0 min-h-0">
        <header className="shrink-0 bg-bg-panel border-b border-line h-14 flex items-center justify-between gap-3 px-5">
          <div className="text-sm font-medium truncate">{session.organization.name}</div>
          <div className="flex items-center gap-2">
            {USE_MOCK && (
              <span className="chip bg-warn/10 text-warn" title="Тестовые данные. Изменения не сохраняются на сервере.">Демонстрация</span>
            )}
            <div className="h-5 w-px bg-line mx-1" />
            <AlertsBell />
            <ThemeToggle />
          </div>
        </header>

        <main key={location.pathname} id="main-content" tabIndex={-1} className="flex-1 min-h-0 overflow-auto">
          <div className="h-full">
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  )
}
