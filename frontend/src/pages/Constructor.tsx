import { useState, useRef, useEffect } from 'react'
import { Boxes, Maximize2, ExternalLink, X, Workflow, Zap, GitBranch, Radio } from 'lucide-react'
import { useTheme } from '@/lib/theme'

// Полноценный конструктор цепочек ловушек — отдельный самостоятельный модуль
// (honeyforge/constructor, бесконечная доска). Встраиваем «как есть» через
// iframe на весь экран. Адрес модуля настраивается через VITE_CONSTRUCTOR_URL.
const CONSTRUCTOR_URL = import.meta.env.VITE_CONSTRUCTOR_URL ?? 'http://127.0.0.1:8077'

const features = [
  { icon: Workflow, title: 'Бесконечная доска', text: 'Собирайте ложную инфраструктуру из блоков: точки входа, сервисы-ловушки, приманки, логика.' },
  { icon: GitBranch, title: 'Цепочки и ветвления', text: 'Соединяйте узлы портами (flow / creds / token / loot), стройте attack-path атакующего.' },
  { icon: Zap, title: 'Симуляция атаки', text: 'Прогон пути нарушителя с таймлайном и маппингом на MITRE ATT&CK.' },
  { icon: Radio, title: 'Боевой режим', text: 'Разворачивайте ловушки и смотрите живой поток атак с анимацией в реальном времени.' },
]

export function ConstructorPage() {
  const [open, setOpen] = useState(false)
  const { theme } = useTheme()
  const iframeRef = useRef<HTMLIFrameElement>(null)

  // Тема панели = тема модуля. Смену темы шлём в iframe вживую (postMessage).
  useEffect(() => {
    iframeRef.current?.contentWindow?.postMessage({ type: 'hf-theme', theme }, '*')
  }, [theme, open])

  return (
    <div className="h-full overflow-y-auto">
      <div className="px-6 py-6 max-w-5xl mx-auto space-y-6">
        <div>
          <h1 className="text-xl font-semibold">Конструктор</h1>
          <p className="text-sm text-muted">
            Визуальный конструктор цепочек ловушек. Открывается на весь экран как отдельный полноценный модуль.
          </p>
        </div>

        {/* Превью-карточка с мокапом доски */}
        <div className="card overflow-hidden">
          <div className="relative aspect-[16/8] bg-bg-base border-b border-line/60">
            {/* фон-сетка */}
            <div
              className="absolute inset-0 opacity-70"
              style={{
                backgroundImage: 'radial-gradient(rgb(var(--line)) 1px, transparent 1px)',
                backgroundSize: '22px 22px',
              }}
            />
            {/* мокап узлов + связей */}
            <MockBoard />

            {/* затемнение + кнопка по центру */}
            <div className="absolute inset-0 grid place-items-center bg-gradient-to-t from-bg-base/70 via-transparent to-transparent">
              <button className="btn-primary text-base px-6 py-3 shadow-glow" onClick={() => setOpen(true)}>
                <Maximize2 size={18} /> Открыть конструктор
              </button>
            </div>
          </div>

          <div className="p-5 flex items-center justify-between gap-4 flex-wrap">
            <div className="flex items-center gap-2 text-sm text-muted">
              <Boxes size={16} className="text-brand-soft" />
              Отдельный модуль · бесконечная доска · реальные данные дашборда
            </div>
            <div className="flex items-center gap-2">
              <a className="btn-ghost" href={CONSTRUCTOR_URL} target="_blank" rel="noreferrer">
                <ExternalLink size={16} /> Открыть в новой вкладке
              </a>
              <button className="btn-primary" onClick={() => setOpen(true)}>
                <Maximize2 size={16} /> На весь экран
              </button>
            </div>
          </div>
        </div>

        {/* Возможности */}
        <div className="grid sm:grid-cols-2 gap-4">
          {features.map((f) => (
            <div key={f.title} className="card p-5 flex gap-3">
              <div className="h-10 w-10 rounded-xl bg-brand/15 grid place-items-center shrink-0">
                <f.icon size={18} className="text-brand-soft" />
              </div>
              <div>
                <div className="font-medium text-fg">{f.title}</div>
                <div className="text-sm text-muted mt-0.5">{f.text}</div>
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* Полноэкранный оверлей с модулем */}
      {open && (
        <div className="fixed inset-0 z-50 flex flex-col bg-bg-base">
          <div className="h-12 shrink-0 flex items-center justify-between px-4 border-b border-line/60 bg-bg-panel/80 backdrop-blur-xl">
            <div className="flex items-center gap-2 text-sm font-medium">
              Конструктор цепочек
            </div>
            <div className="flex items-center gap-2">
              <a className="btn-ghost !py-1.5" href={CONSTRUCTOR_URL} target="_blank" rel="noreferrer">
                <ExternalLink size={15} /> В новой вкладке
              </a>
              <button className="btn-ghost !py-1.5" onClick={() => setOpen(false)}>
                <X size={16} /> Закрыть
              </button>
            </div>
          </div>
          <iframe
            ref={iframeRef}
            title="HoneyForge Конструктор"
            src={`${CONSTRUCTOR_URL}/?v=8&embed=1&theme=${theme}`}
            className="flex-1 w-full border-0"
          />
        </div>
      )}
    </div>
  )
}

// Декоративный мокап доски конструктора (чисто визуальный, без данных).
function MockBoard() {
  const nodes = [
    { x: '8%', y: '38%', c: 'brand', label: 'Internet' },
    { x: '36%', y: '20%', c: 'ok', label: 'SSH-ловушка' },
    { x: '36%', y: '58%', c: 'warn', label: 'Redis' },
    { x: '66%', y: '38%', c: 'danger', label: 'АРМ' },
  ]
  const colors: Record<string, string> = {
    brand: 'border-brand/60 text-brand-soft',
    ok: 'border-ok/60 text-ok',
    warn: 'border-warn/60 text-warn',
    danger: 'border-danger/60 text-danger',
  }
  return (
    <>
      <svg className="absolute inset-0 h-full w-full" fill="none" viewBox="0 0 100 100" preserveAspectRatio="none">
        <path d="M 16 44 C 26 44, 26 26, 36 26" stroke="rgb(var(--line))" strokeWidth="0.6" />
        <path d="M 16 44 C 26 44, 26 64, 36 64" stroke="rgb(var(--line))" strokeWidth="0.6" />
        <path d="M 48 26 C 60 26, 58 44, 66 44" stroke="rgb(var(--danger))" strokeWidth="0.8" strokeDasharray="2 1.6" />
      </svg>
      {nodes.map((n) => (
        <div
          key={n.label}
          className={`absolute w-28 rounded-xl border bg-bg-panel/95 shadow-glass ${colors[n.c]}`}
          style={{ left: n.x, top: n.y }}
        >
          <div className="px-2.5 py-1.5 text-[11px] font-semibold text-fg border-b border-line/50 truncate">
            {n.label}
          </div>
          <div className="px-2.5 py-1.5 flex gap-1">
            <span className="h-1.5 w-1.5 rounded-full bg-current" />
            <span className="h-1.5 w-1.5 rounded-full bg-current opacity-50" />
          </div>
        </div>
      ))}
    </>
  )
}
