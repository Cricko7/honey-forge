import { useEffect, useRef } from 'react'
import { useTheme } from '@/lib/theme'

export function ConstructorPage() {
  const { theme } = useTheme()
  const frame = useRef<HTMLIFrameElement>(null)
  const initialTheme = useRef(theme)
  const sendTheme = () => frame.current?.contentWindow?.postMessage({ type: 'hf-theme', theme }, location.origin)

  useEffect(sendTheme, [theme])

  return (
    <div className="h-full flex flex-col">
      <div className="px-6 py-3 border-b border-line/60">
        <h1 className="text-xl font-semibold">Конструктор ловушек</h1>
        <p className="text-sm text-muted">Соберите TCP-вход, приветствие и правила «запрос → ответ», соедините блоки и нажмите «Развернуть». Запросы заканчиваются переводом строки. Состояние и события появятся на доске.</p>
      </div>
      <iframe ref={frame} title="HoneyForge Конструктор" src={`/constructor/?embed=1&theme=${initialTheme.current}`} onLoad={sendTheme} className="flex-1 w-full border-0" />
    </div>
  )
}
