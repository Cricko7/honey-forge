import { useState } from 'react'
import { Copy } from 'lucide-react'

export function CopyCommand({ command, label }: { command: string; label: string }) {
  const [message, setMessage] = useState('')

  async function copy() {
    try {
      await navigator.clipboard.writeText(command)
      setMessage('Скопировано')
    } catch {
      setMessage('Выделите и скопируйте команду вручную')
    }
  }

  return <div className="space-y-2">
    <pre className="rounded-lg bg-bg-elevated p-3 text-xs font-mono whitespace-pre-wrap break-all select-all">{command}</pre>
    <button type="button" className="btn-ghost" onClick={copy}><Copy size={14} /> {label}</button>
    {message && <p role="status" className="text-xs text-muted">{message}</p>}
  </div>
}
