import { Outlet, Navigate, useNavigate, useLocation } from 'react-router-dom'
import { LogOut, Menu } from 'lucide-react'
import { useSession, useLiveStream } from '@/api/hooks'
import { USE_MOCK, api } from '@/api/client'
import { ThemeToggle } from '@/components/ThemeToggle'
import { useState } from 'react'
import { replaceSessionCache } from '@/api/cache'
import { canManage, canManageTraps, sessionDestination } from '@/lib/auth'
import { usePresence } from '@/lib/presence'
import { ApiError } from '@/api/client'
import { AlertsProvider, AlertsBell, useAlerts } from '@/components/Alerts'
import type { SessionView } from '@/types'
import { SessionScope, useSessionClient } from '@/api/session-scope'
import { Modal } from '@/components/Modal'
import { Navigation } from '@/components/Navigation'
import { Spinner } from '@/components/ui'
import { Sidebar } from '@/components/Sidebar'

export function Layout() {
  const { data: session, isLoading, error, refetch } = useSession()
  const sessionClient = useSessionClient()
  if (isLoading) return <div className="h-screen grid place-items-center"><Spinner label="Проверяем вход…" /></div>
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
  const [menuOpen, setMenuOpen] = useState(false)
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

  const account = (collapsed = false) => <div className="space-y-3 border-t border-line px-2 pt-4">
    <div className={collapsed ? 'sr-only' : 'text-sm font-medium break-all'}>{session.user.email}</div>
    <p className={collapsed ? 'sr-only' : 'text-xs text-muted'}>{canManage(session) ? 'Администратор' : canManageTraps(session) ? 'Оператор ловушек' : 'Только чтение'}</p>
    {logoutError && <p role="alert" className="text-sm text-danger">{logoutError}</p>}
    <button type="button" onClick={logout} disabled={loggingOut} title={collapsed ? 'Выйти' : undefined} className={`btn-ghost w-full ${collapsed ? 'px-0' : ''}`}><LogOut size={16} aria-hidden="true" /> <span className={collapsed ? 'sr-only' : undefined}>{loggingOut ? 'Выход…' : 'Выйти'}</span></button>
  </div>
  return (
    <div className="flex h-dvh flex-col md:flex-row overflow-hidden bg-bg-base">
      <a href="#main-content" className="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-[120] focus:bg-bg-panel focus:p-3">К содержимому</a>
      <Sidebar admin={canManage(session)}>{account}</Sidebar>

      <div className="flex-1 flex flex-col min-w-0 min-h-0">
        <header className="shrink-0 bg-bg-panel border-b border-line h-16 flex items-center justify-between gap-2 px-3 md:px-8">
          <button type="button" className="btn-ghost md:hidden px-3" aria-label="Открыть меню" aria-haspopup="dialog" aria-expanded={menuOpen} onClick={() => setMenuOpen(true)}><Menu size={20} /> <span className="hidden min-[380px]:inline">Меню</span></button>
          <div className="min-w-0 text-sm font-medium truncate">{session.organization.name}</div>
          <div className="flex items-center gap-2">
            {USE_MOCK && (
              <span className="hidden sm:inline-flex chip bg-warn/10 text-warn" title="Тестовые данные. Изменения не сохраняются на сервере.">Демонстрация</span>
            )}
            <div className="h-5 w-px bg-line mx-1" />
            <AlertsBell />
            <ThemeToggle />
          </div>
        </header>
        <Modal open={menuOpen} onClose={() => setMenuOpen(false)} title="Меню HoneyForge">
          <p className="mb-4 text-sm font-medium break-words">{session.organization.name}{USE_MOCK && <span className="ml-2 chip bg-warn/10 text-warn">Демонстрация</span>}</p>
          <Navigation admin={canManage(session)} onNavigate={() => setMenuOpen(false)} />
          <div className="mt-4">{account()}</div>
        </Modal>

        <main key={location.pathname} id="main-content" tabIndex={-1} className="flex-1 min-h-0 overflow-auto">
          <div className="h-full">
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  )
}
