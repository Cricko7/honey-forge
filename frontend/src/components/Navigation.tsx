import { NavLink } from 'react-router-dom'
import { LayoutDashboard, Server, Bug, ScrollText, Boxes, Workflow, Network, FileSliders, Link2, ShieldCheck } from 'lucide-react'
import { cn } from '@/lib/utils'

const groups = [
  { title: 'Мониторинг', items: [
    { to: '/app', end: true, icon: LayoutDashboard, label: 'Обзор' },
    { to: '/app/traps', end: false, icon: Bug, label: 'Ловушки' },
    { to: '/app/events', end: false, icon: ScrollText, label: 'События' },
    { to: '/app/network', end: false, icon: Network, label: 'Конструктор сети' },
  ] },
  { title: 'Настройка ловушек', items: [
    { to: '/app/constructor', end: false, icon: Workflow, label: 'Конструктор ловушек' },
    { to: '/app/profiles', end: false, icon: FileSliders, label: 'Профили' },
    { to: '/app/catalog', end: false, icon: Boxes, label: 'Каталог типов' },
    { to: '/app/servers', end: false, icon: Server, label: 'Серверы' },
  ] },
  { title: 'Управление', items: [
    { to: '/app/admin', end: false, icon: ShieldCheck, label: 'Админ-панель' },
    { to: '/app/organization', end: false, icon: Link2, label: 'Приглашения' },
  ] },
]

export function Navigation({ admin, onNavigate }: { admin: boolean; onNavigate?: () => void }) {
  return <nav aria-label="Основная навигация" className="space-y-6">
    {groups.filter(group => admin || group.title !== 'Управление').map(group => <div key={group.title}>
      <div className="px-3 mb-2 text-xs font-medium text-muted">{group.title}</div>
      {group.items.map(item => <NavLink key={item.to} to={item.to} end={item.end} onClick={onNavigate}
        className={({ isActive }) => cn('flex min-h-11 items-center gap-3 px-3 py-2 rounded-lg text-sm font-medium transition-colors',
          isActive ? 'bg-brand/10 text-brand' : 'text-muted hover:text-fg hover:bg-bg-hover')}>
        <item.icon size={18} aria-hidden="true" className="shrink-0" />{item.label}
      </NavLink>)}
    </div>)}
  </nav>
}
