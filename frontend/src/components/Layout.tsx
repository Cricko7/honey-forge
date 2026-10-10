import { NavLink, Outlet, useNavigate } from 'react-router-dom'
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

const nav = [
  { to: '/app', end: true, icon: LayoutDashboard, label: 'Дашборд' },
  { to: '/app/network', icon: Network, label: 'Конструктор сети' },
  { to: '/app/traps', icon: Bug, label: 'Ловушки' },
  { to: '/app/events', icon: ScrollText, label: 'События' },
  { to: '/app/profiles', icon: FileSliders, label: 'Профили' },
  { to: '/app/catalog', icon: Boxes, label: 'Каталог типов' },
]

export function Layout() {
  const { data: session } = useSession()
  const navigate = useNavigate()
  // Глобальная подписка на live-стрим: держит кэш свежим на всех экранах.
  useLiveStream()

  async function logout() {
    await api.logout().catch(() => {})
    navigate('/login')
  }

  return (
    <div className="flex h-screen overflow-hidden">
      {/* Боковое меню — следует теме (светлое в светлой, тёмное в тёмной) */}
      <aside className="w-60 shrink-0 bg-bg-panel/90 text-fg border-r border-line/60 flex flex-col backdrop-blur-xl">
        <div className="px-5 h-16 flex items-center gap-2.5 border-b border-line/60">
          <div className="h-8 w-8 rounded-xl bg-brand/15 grid place-items-center shadow-glow">
            <Shield size={18} className="text-brand" />
          </div>
          <div className="leading-tight">
            <div className="font-semibold text-fg">HoneyForge</div>
            <div className="text-[10px] text-muted">deception platform</div>
          </div>
        </div>

        <nav className="flex-1 px-3 py-4 space-y-1">
          {nav.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.end}
              className={({ isActive }) =>
                cn(
                  'flex items-center gap-3 px-3 py-2.5 rounded-xl text-sm font-medium transition-colors',
                  isActive
                    ? 'bg-brand/15 text-brand'
                    : 'text-muted hover:text-fg hover:bg-bg-hover',
                )
              }
            >
              <item.icon size={18} />
              {item.label}
            </NavLink>
          ))}
        </nav>

        <div className="p-3 border-t border-line/60">
          <div className="px-3 py-2 mb-2">
            <div className="text-sm text-fg truncate">{session?.user.email ?? '—'}</div>
            <div className="text-xs text-muted">
              {session?.organization.name} · {session?.user.role}
            </div>
          </div>
          <button onClick={logout} className="btn-ghost w-full">
            <LogOut size={16} /> Выйти
          </button>
        </div>
      </aside>

      <div className="flex-1 flex flex-col min-w-0">
        <header className="h-16 shrink-0 border-b border-line/60 bg-bg-panel/50 backdrop-blur-xl flex items-center justify-between px-6">
          <div className="flex items-center gap-2 text-sm">
            <span className="chip bg-ok/10 text-ok">
              <span className="h-1.5 w-1.5 rounded-full bg-ok animate-pulseGlow" /> System active
            </span>
            {USE_MOCK && <span className="chip bg-warn/10 text-warn">demo · mock-данные</span>}
          </div>
          <div className="flex items-center gap-4">
            <div className="flex items-center gap-2 text-xs text-muted">
              <Radio size={14} className="text-brand-soft animate-pulseGlow" />
              live-поток подключён
            </div>
            <ThemeToggle />
          </div>
        </header>

        <main className="flex-1 overflow-auto">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
