import { useCallback, useEffect, useRef, useState } from 'react'
import { BookOpen } from 'lucide-react'
import { useTheme } from '@/lib/theme'
import { ConstructorGuide } from './constructor/ConstructorGuide'

export function ConstructorPage() {
  const { theme } = useTheme()
  const [guideOpen, setGuideOpen] = useState(false)
  const closeGuide = useCallback(() => setGuideOpen(false), [])
  const frame = useRef<HTMLIFrameElement>(null)
  const initialTheme = useRef(theme)
  const sendTheme = () => frame.current?.contentWindow?.postMessage({ type: 'hf-theme', theme }, location.origin)

  useEffect(sendTheme, [theme])

  return (
    <div className="h-full flex flex-col">
      <div className="px-6 py-3 border-b border-line/60 flex items-start justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold">Конструктор ловушек</h1>
          <p className="text-sm text-muted">Соберите поведение ловушки из блоков, соедините их и нажмите «Развернуть». Начните с гайда, если создаёте первую цепочку.</p>
        </div>
        <button type="button" className="btn-ghost shrink-0" onClick={() => setGuideOpen(true)}><BookOpen size={16} /> Как собрать ловушку</button>
      </div>
      <iframe ref={frame} title="HoneyForge Конструктор" src={`/constructor/?embed=1&theme=${initialTheme.current}`} onLoad={sendTheme} className="flex-1 w-full border-0" />
      <ConstructorGuide open={guideOpen} onClose={closeGuide} />
    </div>
  )
}
