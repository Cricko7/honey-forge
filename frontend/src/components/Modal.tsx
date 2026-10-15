import { useId, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'

export function Modal({
  open,
  onClose,
  title,
  children,
  width = 'max-w-lg',
}: {
  open: boolean
  onClose: () => void
  title: string
  children: ReactNode
  width?: string
}) {
  const titleID = useId()
  if (!open) return null
  const dialog = (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={onClose} />
      <div role="dialog" aria-modal="true" aria-labelledby={titleID} className={`relative w-full ${width} card p-0 flex flex-col max-h-[calc(100dvh-2rem)] overflow-hidden animate-slideIn`}>
        <div className="shrink-0 flex items-center justify-between gap-3 px-5 py-4 border-b border-line/60">
          <h3 id={titleID} className="text-sm font-semibold text-fg min-w-0 break-words">{title}</h3>
          <button type="button" aria-label="Закрыть окно" onClick={onClose} className="p-2 -mr-2 rounded-md text-muted hover:text-fg transition-colors">
            <X size={18} />
          </button>
        </div>
        <div className="p-5 min-h-0 overflow-y-auto">{children}</div>
      </div>
    </div>
  )
  return typeof document === 'undefined' ? dialog : createPortal(dialog, document.body)
}
