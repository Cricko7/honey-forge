import { useLayoutEffect, useRef, useState, type ReactNode, type RefObject } from 'react'
import { createPortal } from 'react-dom'

export function PickerPopover({ anchor, open, onClose, children, width }: {
  anchor: RefObject<HTMLElement>; open: boolean; onClose: () => void; children: ReactNode; width?: number
}) {
  const panel = useRef<HTMLDivElement>(null)
  const [position, setPosition] = useState<React.CSSProperties>({})

  useLayoutEffect(() => {
    if (!open || !anchor.current) return
    const place = () => {
      const rect = anchor.current!.getBoundingClientRect()
      const size = Math.min(width ?? rect.width, window.innerWidth - 24)
      const below = window.innerHeight - rect.bottom - 20
      const above = rect.top - 20
      const upwards = below < Math.min(panel.current?.scrollHeight ?? 320, 380) && above > below
      setPosition({
        width: size, left: Math.max(12, Math.min(rect.left, window.innerWidth - size - 12)),
        top: upwards ? undefined : rect.bottom + 8,
        bottom: upwards ? window.innerHeight - rect.top + 8 : undefined,
        maxHeight: Math.max(80, upwards ? above : below),
      })
    }
    const outside = (event: Event) => {
      if (!panel.current?.contains(event.target as Node) && !anchor.current?.contains(event.target as Node)) onClose()
    }
    const escape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') { event.preventDefault(); onClose(); anchor.current?.focus() }
    }
    place()
    document.addEventListener('pointerdown', outside)
    document.addEventListener('focusin', outside)
    document.addEventListener('keydown', escape)
    window.addEventListener('resize', place)
    window.addEventListener('scroll', place, true)
    return () => {
      document.removeEventListener('pointerdown', outside)
      document.removeEventListener('focusin', outside)
      document.removeEventListener('keydown', escape)
      window.removeEventListener('resize', place)
      window.removeEventListener('scroll', place, true)
    }
  }, [open, anchor, width, onClose])

  if (!open) return null
  return createPortal(<div ref={panel} style={position} className="fixed z-[100] overflow-auto rounded-xl border border-line bg-bg-panel p-2 text-fg animate-fadeIn">{children}</div>, anchor.current?.closest('dialog') ?? document.body)
}
