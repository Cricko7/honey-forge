import { createContext, useCallback, useContext, useEffect, useReducer, useRef, useState, type Dispatch, type SetStateAction, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { Bell, CheckCheck, CircleCheck, Info, ShieldAlert, Trash2, TriangleAlert, X } from 'lucide-react'
import { alertReducer, createAlertTracker, type AlertInput, type AlertItem } from '@/lib/alerts'
import { subscribeMutationAlerts } from '@/api/alert-mutations'
import { useTraps } from '@/api/hooks'
import { cn, formatDateTime } from '@/lib/utils'
import { PickerPopover } from './PickerPopover'
import { MobileAlertSummary } from './MobileAlertSummary'
import type { StreamNotification } from '@/types'

const icons = { info: Info, success: CircleCheck, warning: ShieldAlert, error: TriangleAlert }
const colors = { info: 'text-info bg-info/10', success: 'text-ok bg-ok/10', warning: 'text-warn bg-warn/10', error: 'text-danger bg-danger/10' }
const AlertsContext = createContext<{
  items: AlertItem[]
  notify: (alert: AlertInput) => void
  onStream: (notification: StreamNotification) => void
  dismiss: (id: string) => void
  read: (id: string) => void
  readAll: () => void
  clear: () => void
  historyOpen: boolean
  setHistoryOpen: Dispatch<SetStateAction<boolean>>
} | null>(null)

export function useAlerts() {
  const value = useContext(AlertsContext)
  if (!value) throw new Error('AlertsProvider is required')
  return value
}

export function AlertsProvider({ children }: { children: ReactNode }) {
  const [items, dispatch] = useReducer(alertReducer, [])
  const [historyOpen, setHistoryOpen] = useState(false)
  const [tracker] = useState(createAlertTracker)
  const qc = useQueryClient()
  const { data: traps } = useTraps()
  const notify = useCallback((alert: AlertInput) => dispatch({ type: 'add', alert, now: Date.now() }), [])
  const dismiss = useCallback((id: string) => dispatch({ type: 'dismiss', id }), [])
  const read = useCallback((id: string) => dispatch({ type: 'read', id }), [])
  useEffect(() => { if (traps) tracker.seed(traps.items) }, [traps, tracker])
  useEffect(() => subscribeMutationAlerts(qc.getMutationCache(), notify), [qc, notify])
  const onStream = useCallback((n: StreamNotification) => {
    for (const alert of tracker.accept(n)) notify(alert)
  }, [tracker, notify])
  return <AlertsContext.Provider value={{ items, notify, onStream, dismiss, read,
    historyOpen, setHistoryOpen,
    readAll: () => dispatch({ type: 'read-all' }), clear: () => dispatch({ type: 'clear' }) }}>
    {children}
    <div className={cn('fixed bottom-4 right-4 sm:bottom-6 sm:right-6 z-40 w-[380px] max-w-[calc(100%-2rem)]', historyOpen && 'hidden')} aria-label="Всплывающие уведомления">
      <MobileAlertSummary items={items} onOpen={() => setHistoryOpen(true)} />
      <div className="hidden sm:block space-y-3 max-h-[calc(100dvh-2rem)] overflow-y-auto">
        {items.filter((a) => a.toast).map((a) => <AlertToast key={a.id} item={a} />)}
      </div>
    </div>
  </AlertsContext.Provider>
}

function AlertIcon({ level, severity }: { level: AlertInput['level']; severity?: AlertInput['severity'] }) {
  const Icon = icons[level]
  return <span className={cn('h-9 w-9 shrink-0 grid place-items-center rounded-xl', severity ? `event-label-${severity}` : colors[level])}><Icon size={18} /></span>
}

function AlertToast({ item }: { item: AlertItem }) {
  const { dismiss, read } = useAlerts()
  const [paused, setPaused] = useState(false)
  useEffect(() => {
    if (paused) return
    const timer = setTimeout(() => dismiss(item.id), 7000)
    return () => clearTimeout(timer)
  }, [item.id, dismiss, paused])
  return <div role={item.level === 'error' ? 'alert' : 'status'}
    onMouseEnter={() => setPaused(true)} onMouseLeave={() => setPaused(false)}
    onFocus={() => setPaused(true)} onBlur={(e) => { if (!e.currentTarget.contains(e.relatedTarget)) setPaused(false) }}
    className={cn("card p-4 flex items-start gap-3 animate-fadeInUp", item.severity && `event-surface event-surface-${item.severity}`)}>
    <AlertIcon level={item.level} severity={item.severity} />
    <div className="min-w-0 flex-1">
      <p className="text-sm font-semibold break-words">{item.title}</p>
      {item.message && <p className="mt-1 text-xs text-muted break-words">{item.message}</p>}
      {item.href && <Link to={item.href} onClick={() => read(item.id)} className="inline-block text-xs text-brand font-medium mt-2 hover:underline">Открыть детали</Link>}
    </div>
    <button type="button" aria-label="Закрыть уведомление" onClick={() => dismiss(item.id)} className="p-1 rounded-md text-muted hover:bg-bg-hover hover:text-fg"><X size={15} /></button>
  </div>
}

export function AlertsBell() {
  const { items, read, readAll, clear, historyOpen: open, setHistoryOpen: setOpen } = useAlerts()
  const anchor = useRef<HTMLButtonElement>(null)
  const panel = useRef<HTMLElement>(null)
  const close = useCallback(() => setOpen(false), [setOpen])
  const unread = items.filter((a) => !a.read).length
  useEffect(() => { if (open) panel.current?.focus() }, [open])
  return <>
    <button ref={anchor} type="button" aria-label={`Алерты${unread ? `: ${unread} непрочитанных` : ''}`} title="Алерты"
      aria-haspopup="dialog" aria-expanded={open} aria-controls={open ? 'alerts-panel' : undefined}
      onClick={() => setOpen((value) => !value)} className="relative h-11 w-11 shrink-0 rounded-lg grid place-items-center text-muted hover:text-fg hover:bg-bg-hover transition-colors">
      <Bell size={19} />
      {unread > 0 && <span className="absolute -top-1 -right-1 min-w-4 h-4 px-1 rounded-full bg-danger text-white text-[9px] font-semibold grid place-items-center">{unread > 99 ? '99+' : unread}</span>}
    </button>
    <PickerPopover anchor={anchor} open={open} onClose={close} width={420}>
      <section id="alerts-panel" ref={panel} role="dialog" aria-label="Алерты" tabIndex={-1} className="outline-none">
        <div className="flex items-center justify-between px-3 py-3">
          <div><h2 className="font-semibold text-sm">Алерты</h2><p className="text-xs text-muted mt-1">{unread ? `${unread} непрочитанных` : 'Все прочитаны'} · текущий сеанс</p></div>
          <button type="button" aria-label="Закрыть историю алертов" onClick={() => { close(); anchor.current?.focus() }} className="p-2 rounded-lg text-muted hover:bg-bg-hover"><X size={16} /></button>
        </div>
        {items.length > 0 && <div className="flex gap-2 px-3 pb-3 border-b border-line">
          <button type="button" onClick={readAll} disabled={!unread} className="btn-ghost !text-xs !px-2 !py-1.5"><CheckCheck size={14} /> Прочитать все</button>
          <button type="button" onClick={clear} className="btn-ghost !text-xs !px-2 !py-1.5"><Trash2 size={14} /> Очистить</button>
        </div>}
        <div className="max-h-[min(440px,60vh)] overflow-y-auto">
          {items.length === 0 ? <div className="py-10 px-6 text-center"><Bell size={26} className="mx-auto text-muted mb-3" /><p className="text-sm font-medium">Пока нет алертов</p><p className="text-xs text-muted mt-2">Здесь появятся новые события и результаты действий.</p></div>
            : items.map((item) => <div key={item.id} className={cn('flex items-start gap-3 rounded-xl p-3 my-1', item.severity ? `event-surface event-surface-${item.severity}` : !item.read && 'bg-brand/5')}>
              <AlertIcon level={item.level} severity={item.severity} />
              <div className="flex-1 min-w-0">
                {item.href ? <Link to={item.href} onClick={() => { read(item.id); close() }} className="text-sm font-medium hover:text-brand break-words">{item.title}</Link> : <p className="text-sm font-medium break-words">{item.title}</p>}
                {item.message && <p className="mt-1 text-xs text-muted break-words">{item.message}</p>}
                <time dateTime={new Date(item.createdAt).toISOString()} className="block mt-2 text-[10px] text-muted">{formatDateTime(new Date(item.createdAt).toISOString())}</time>
              </div>
              {!item.read && <button type="button" aria-label={`Прочитать: ${item.title}`} onClick={() => read(item.id)} className="p-1 rounded-md text-brand hover:bg-bg-hover"><CheckCheck size={15} /></button>}
            </div>)}
        </div>
      </section>
    </PickerPopover>
  </>
}
