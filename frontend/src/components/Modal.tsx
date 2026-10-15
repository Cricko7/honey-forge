import { useEffect, useId, useRef, type ReactNode } from 'react'
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
  const dialogRef = useRef<HTMLDialogElement>(null)
  const closeRef = useRef(onClose)
  closeRef.current = onClose

  useEffect(() => {
    const element = dialogRef.current
    if (!open || !element) return
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
    element.showModal()
    const cancel = (event: Event) => {
      event.preventDefault()
      closeRef.current()
    }
    element.addEventListener('cancel', cancel)
    return () => {
      element.removeEventListener('cancel', cancel)
      element.close()
      if (previous?.isConnected) previous.focus()
    }
  }, [open])

  if (!open) return null
  const dialog = (
    <dialog ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby={titleID}
      className={`m-auto w-[calc(100%-2rem)] ${width} card p-0 text-fg max-h-[calc(100dvh-2rem)] overflow-visible backdrop:bg-black/60`}>
      <div className="flex flex-col max-h-[calc(100dvh-2rem)] overflow-hidden rounded-xl">
        <div className="shrink-0 flex items-center justify-between gap-3 px-6 py-4 border-b border-line">
          <h3 id={titleID} className="text-base font-semibold text-fg min-w-0 break-words">{title}</h3>
          <button type="button" aria-label="Закрыть окно" onClick={onClose} className="min-h-11 min-w-11 grid place-items-center -mr-2 rounded-lg text-muted hover:bg-bg-hover hover:text-fg transition-colors">
            <X size={18} />
          </button>
        </div>
        <div className="p-6 min-h-0 overflow-y-auto">{children}</div>
      </div>
    </dialog>
  )
  return typeof document === 'undefined' ? dialog : createPortal(dialog, document.body)
}
