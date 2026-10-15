import { useCallback, useEffect, useId, useRef, useState } from 'react'
import { Check, ChevronDown } from 'lucide-react'
import { cn } from '@/lib/utils'
import { PickerPopover } from './PickerPopover'

export interface SelectOption { value: string; label: string; disabled?: boolean }

export function Select({ value, onChange, options, label, id, className, disabled }: {
  value: string; onChange: (value: string) => void; options: SelectOption[]; label: string
  id?: string; className?: string; disabled?: boolean
}) {
  const generatedId = useId()
  const listId = `${generatedId}-options`
  const anchor = useRef<HTMLButtonElement>(null)
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(0)
  const close = useCallback(() => setOpen(false), [])
  const selected = options.find((option) => option.value === value)
  const show = () => {
    setActive(Math.max(0, options.findIndex((option) => option.value === value && !option.disabled)))
    setOpen(true)
  }
  const choose = (index: number) => {
    if (!options[index] || options[index].disabled) return
    onChange(options[index].value)
    close()
    anchor.current?.focus()
  }

  useEffect(() => {
    if (open) document.getElementById(`${listId}-${active}`)?.scrollIntoView({ block: 'nearest' })
  }, [open, active, listId])

  return <>
    <button ref={anchor} id={id} type="button" role="combobox" aria-label={label} aria-expanded={open}
      aria-controls={listId} aria-haspopup="listbox" aria-activedescendant={open ? `${listId}-${active}` : undefined}
      disabled={disabled} className={cn('input flex items-center justify-between gap-2 text-left disabled:opacity-50', open && 'border-brand ring-2 ring-brand/20', className)}
      onClick={() => open ? close() : show()}
      onKeyDown={(event) => {
        if (event.key === 'Tab') { close(); return }
        if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
          event.preventDefault()
          if (!open) { show(); return }
          const direction = event.key === 'ArrowDown' ? 1 : -1
          for (let offset = 1; offset <= options.length; offset++) {
            const index = (active + direction * offset + options.length) % options.length
            if (!options[index].disabled) { setActive(index); break }
          }
        } else if (event.key === 'Home' || event.key === 'End') {
          event.preventDefault()
          const indices = options.map((_, index) => index).filter((index) => !options[index].disabled)
          setActive((event.key === 'Home' ? indices[0] : indices.at(-1)) ?? 0)
          setOpen(true)
        } else if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault()
          if (open) choose(active); else show()
        } else if (event.key.length === 1) {
          const index = options.findIndex((option) => !option.disabled && option.label.toLocaleLowerCase().startsWith(event.key.toLocaleLowerCase()))
          if (index >= 0) { setActive(index); setOpen(true) }
        }
      }}>
      <span className="truncate">{selected?.label ?? 'Выберите значение'}</span>
      <ChevronDown size={16} className={cn('shrink-0 text-muted transition-transform', open && 'rotate-180')} />
    </button>
    <PickerPopover anchor={anchor} open={open} onClose={close}>
      <div id={listId} role="listbox" aria-label={label} className="max-h-64 space-y-1 overflow-auto">
        {options.map((option, index) => <button key={option.value} id={`${listId}-${index}`} type="button" role="option"
          aria-selected={option.value === value} aria-disabled={option.disabled} disabled={option.disabled} tabIndex={-1}
          onMouseEnter={() => { if (!option.disabled) setActive(index) }} onClick={() => choose(index)}
          className={cn('flex min-h-11 w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left text-sm transition-colors disabled:cursor-not-allowed disabled:opacity-40',
            active === index ? 'bg-brand/10 text-brand' : 'hover:bg-bg-hover', option.value === value && 'font-medium')}>
          <span className="min-w-0 flex-1 break-words">{option.label}</span>
          {option.value === value && <Check size={15} className="shrink-0 text-brand" />}
        </button>)}
        {options.length === 0 && <p className="px-3 py-2 text-sm text-muted">Нет доступных вариантов</p>}
      </div>
    </PickerPopover>
  </>
}
