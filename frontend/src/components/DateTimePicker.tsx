import { useCallback, useRef, useState } from 'react'
import { CalendarDays, ChevronLeft, ChevronRight, Clock3 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { calendarDays, localDateTime } from '@/lib/calendar'
import { PickerPopover } from './PickerPopover'

export function DateTimePicker({ value, onChange, label, className }: {
  value: string; onChange: (value: string) => void; label: string; className?: string
}) {
  const anchor = useRef<HTMLButtonElement>(null)
  const [open, setOpen] = useState(false)
  const [view, setView] = useState(() => new Date())
  const [draft, setDraft] = useState('')
  const close = useCallback(() => setOpen(false), [])
  const selected = draft ? new Date(draft) : null
  const today = new Date()
  const days = calendarDays(view.getFullYear(), view.getMonth())
  const show = () => {
    setDraft(value)
    setView(value ? new Date(value) : new Date())
    setOpen(true)
  }
  const selectDay = (day: Date) => {
    const date = new Date(day)
    date.setHours(selected?.getHours() ?? 0, selected?.getMinutes() ?? 0, 0, 0)
    setDraft(localDateTime(date))
    setView(date)
  }
  const setTime = (part: 'hours' | 'minutes', text: string) => {
    const number = Number(text)
    if (!Number.isInteger(number) || number < 0 || number > (part === 'hours' ? 23 : 59)) return
    const date = selected ? new Date(selected) : new Date(view.getFullYear(), view.getMonth(), 1)
    if (part === 'hours') date.setHours(number); else date.setMinutes(number)
    setDraft(localDateTime(date))
  }
  const sameDay = (a: Date, b: Date) => a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate()
  const apply = () => { onChange(draft); close(); anchor.current?.focus() }

  return <>
    <button ref={anchor} type="button" aria-label={label} aria-haspopup="dialog" aria-expanded={open}
      onClick={() => open ? close() : show()} className={cn('input flex items-center justify-between gap-2 text-left', open && 'border-brand ring-2 ring-brand/20', className)}>
      <span className={cn('truncate', !value && 'text-muted')}>{value ? new Date(value).toLocaleString('ru-RU', { day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit' }) : 'Выберите дату'}</span>
      <CalendarDays size={16} className="shrink-0 text-muted" />
    </button>
    <PickerPopover anchor={anchor} open={open} onClose={close} width={320}>
      <div role="dialog" aria-label={`Календарь: ${label}`} className="p-2">
        <div className="mb-2 flex items-center justify-between">
          <span className="pl-1 text-sm font-semibold capitalize">{view.toLocaleDateString('ru-RU', { month: 'long', year: 'numeric' })}</span>
          <div className="flex gap-1">
            <button type="button" aria-label="Предыдущий месяц" className="rounded-lg p-2 text-muted hover:bg-bg-hover hover:text-fg" onClick={() => setView(new Date(view.getFullYear(), view.getMonth() - 1, 1))}><ChevronLeft size={16} /></button>
            <button type="button" aria-label="Следующий месяц" className="rounded-lg p-2 text-muted hover:bg-bg-hover hover:text-fg" onClick={() => setView(new Date(view.getFullYear(), view.getMonth() + 1, 1))}><ChevronRight size={16} /></button>
          </div>
        </div>
        <div className="mb-2 grid grid-cols-7 text-center text-[11px] font-medium text-muted">
          {['Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб', 'Вс'].map((day) => <span key={day}>{day}</span>)}
        </div>
        <div className="grid grid-cols-7 gap-0.5">
          {days.map((day) => {
            const chosen = selected && sameDay(day, selected)
            return <button key={localDateTime(day)} type="button" aria-label={day.toLocaleDateString('ru-RU', { day: 'numeric', month: 'long', year: 'numeric' })} aria-pressed={!!chosen}
              autoFocus={sameDay(day, selected ?? today)}
              onKeyDown={(event) => {
                const offset = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 }[event.key]
                if (offset === undefined) return
                event.preventDefault()
                const buttons = Array.from(event.currentTarget.parentElement!.querySelectorAll('button'))
                const index = buttons.indexOf(event.currentTarget)
                buttons[index + offset]?.focus()
              }}
              onClick={() => selectDay(day)} className={cn('h-8 rounded-lg text-xs transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand',
                chosen ? 'bg-brand font-semibold text-white shadow-sm' : 'hover:bg-brand/10 hover:text-brand',
                !chosen && day.getMonth() !== view.getMonth() && 'text-muted/50',
                !chosen && sameDay(day, today) && 'bg-brand/10 font-semibold text-brand ring-1 ring-brand/30')}>
              {day.getDate()}
            </button>
          })}
        </div>
        <div className="mt-3 flex items-center justify-between border-t border-line pt-3">
          <span className="flex items-center gap-2 text-xs text-muted"><Clock3 size={14} />Время</span>
          <div className="flex items-center gap-2">
            <input aria-label="Часы" type="number" min={0} max={23} value={selected?.getHours() ?? 0} onChange={(event) => setTime('hours', event.target.value)} className="input !w-16 text-center tabular-nums" />
            <span className="text-muted">:</span>
            <input aria-label="Минуты" type="number" min={0} max={59} value={selected?.getMinutes() ?? 0} onChange={(event) => setTime('minutes', event.target.value)} className="input !w-16 text-center tabular-nums" />
          </div>
        </div>
        <div className="mt-3 flex items-center justify-between gap-2">
          <button type="button" className="rounded-lg px-2 py-2 text-xs text-brand hover:bg-brand/10" onClick={() => { setDraft(localDateTime(new Date())); setView(new Date()) }}>Сегодня</button>
          <button type="button" className="rounded-lg px-2 py-2 text-xs text-muted hover:bg-bg-hover" onClick={() => { onChange(''); close(); anchor.current?.focus() }}>Очистить</button>
          <button type="button" className="btn-primary !px-3 !py-2 !text-xs" disabled={!draft} onClick={apply}>Применить</button>
        </div>
      </div>
    </PickerPopover>
  </>
}
