import { useCallback, useEffect, useRef, useState } from 'react'
import { BookOpen } from 'lucide-react'
import { useTheme } from '@/lib/theme'
import { ConstructorGuide } from './constructor/ConstructorGuide'
import { USE_MOCK } from '@/api/client'

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
      <div className="px-6 py-3 border-b border-line/60 flex flex-wrap shrink-0 items-start justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold">Конструктор ловушек</h1>
          <p className="text-sm text-muted">Соедините блоки поведения и нажмите «Развернуть».</p>
        </div>
        <button type="button" className="btn-ghost shrink-0" onClick={() => setGuideOpen(true)}><BookOpen size={16} /> Как собрать ловушку</button>
      </div>
      {USE_MOCK ? <div className="p-6">
        <div className="card max-w-xl p-5 space-y-2">
          <h2 className="font-semibold">Конструктор доступен при подключении к серверу</h2>
          <p className="text-sm text-muted">Доски сохраняются на сервере и требуют входа в организацию. В демонстрации доступно руководство «Как собрать ловушку» с примером настройки и проверки.</p>
        </div>
      </div> : <iframe ref={frame} title="HoneyForge Конструктор" src={`/constructor/?embed=1&theme=${initialTheme.current}`} onLoad={sendTheme} className="flex-1 min-h-[320px] w-full border-0" />}
      <ConstructorGuide open={guideOpen} onClose={closeGuide} />
    </div>
  )
}
