import { useState, type ReactNode } from 'react'
import { PanelLeftClose, PanelLeftOpen } from 'lucide-react'
import { Logo } from '@/components/Logo'
import { Navigation } from '@/components/Navigation'
import { cn } from '@/lib/utils'

const preferenceKey = 'honeyforge.sidebar.collapsed'

function readCollapsed() {
  try {
    return localStorage.getItem(preferenceKey) === 'true'
  } catch {
    return false
  }
}

export function Sidebar({ admin, children }: { admin: boolean; children: (collapsed: boolean) => ReactNode }) {
  const [collapsed, setCollapsed] = useState(readCollapsed)
  const label = collapsed ? 'Развернуть панель' : 'Свернуть панель'

  function toggle() {
    const next = !collapsed
    setCollapsed(next)
    try {
      localStorage.setItem(preferenceKey, String(next))
    } catch {
      // The panel remains usable when browser storage is blocked or full.
    }
  }

  return <aside aria-label="Боковая панель" className={cn('hidden md:flex shrink-0 bg-bg-panel border-r border-line flex-col min-h-0', collapsed ? 'w-20' : 'w-64')}>
    <div className={cn('h-16 shrink-0 flex items-center border-b border-line', collapsed ? 'justify-center px-2' : 'gap-2 px-3')}>
      {!collapsed && <><Logo className="shrink-0" /><span className="font-semibold text-fg">HoneyForge</span></>}
      <button type="button" onClick={toggle} aria-label={label} title={label} aria-expanded={!collapsed} aria-controls="desktop-navigation"
        className={cn('btn-ghost min-w-11 px-2', !collapsed && 'ml-auto')}>
        {collapsed ? <PanelLeftOpen size={20} aria-hidden="true" /> : <PanelLeftClose size={20} aria-hidden="true" />}
      </button>
    </div>
    <div id="desktop-navigation" className="flex-1 overflow-y-auto px-3 py-5"><Navigation admin={admin} collapsed={collapsed} /></div>
    <div className="px-3 pb-4">{children(collapsed)}</div>
  </aside>
}
