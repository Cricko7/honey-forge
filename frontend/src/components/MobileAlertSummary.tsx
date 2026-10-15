import { Bell, ChevronRight } from 'lucide-react'
import type { AlertItem } from '@/lib/alerts'

export function MobileAlertSummary({ items, onOpen }: { items: AlertItem[]; onOpen: () => void }) {
  const visible = items.filter(item => item.toast)
  // Keep active failures prominent even when routine stream events arrive later.
  const latest = visible.find(item => item.level === 'error') ?? visible[0]
  if (!latest) return null
  const unread = items.filter(item => !item.read).length
  return <div className="sm:hidden" role={latest.level === 'error' ? 'alert' : 'status'}>
    <button type="button" onClick={onOpen} aria-haspopup="dialog" aria-label={`Открыть историю алертов: ${unread} непрочитанных. ${latest.title}`}
      className="card w-full !p-3 flex items-center gap-3 text-left">
      <Bell size={18} className={`shrink-0 ${latest.level === 'error' ? 'text-danger' : 'text-warn'}`} />
      <span className="min-w-0 flex-1">
        <span className="block text-xs text-muted">Алерты · {unread} непрочитанных</span>
        <span className="block truncate text-sm font-medium">{latest.title}</span>
      </span>
      <ChevronRight size={16} className="shrink-0 text-muted" />
    </button>
  </div>
}
