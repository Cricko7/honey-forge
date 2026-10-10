import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'

export type Theme = 'light' | 'dark'

interface ThemeCtx {
  theme: Theme
  toggle: () => void
  setTheme: (t: Theme) => void
}

const Ctx = createContext<ThemeCtx | null>(null)
const KEY = 'honeyforge.theme'

function readSaved(): Theme {
  const saved = localStorage.getItem(KEY) as Theme | null
  if (saved === 'light' || saved === 'dark') return saved
  return 'light' // SPI светлая по умолчанию
}

// Применяем атрибут синхронно, чтобы getComputedStyle в графиках сразу
// видел новые CSS-переменные на том же рендере.
function apply(t: Theme) {
  document.documentElement.setAttribute('data-theme', t)
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setThemeState] = useState<Theme>(() => {
    const t = readSaved()
    apply(t)
    return t
  })

  useEffect(() => {
    localStorage.setItem(KEY, theme)
  }, [theme])

  const setTheme = (t: Theme) => {
    apply(t)
    setThemeState(t)
  }
  const toggle = () => setTheme(theme === 'light' ? 'dark' : 'light')

  return <Ctx.Provider value={{ theme, toggle, setTheme }}>{children}</Ctx.Provider>
}

export function useTheme(): ThemeCtx {
  const ctx = useContext(Ctx)
  if (!ctx) throw new Error('useTheme must be used within ThemeProvider')
  return ctx
}

function readToken(name: string, fallback: string): string {
  const raw = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return raw ? `rgb(${raw})` : fallback
}

export interface ThemeColors {
  brand: string
  brandSoft: string
  line: string
  panel: string
  elevated: string
  muted: string
  danger: string
  mask: string
}

// Цвета для Recharts / React Flow из текущих CSS-переменных.
// Пересчитываются при смене темы (завязаны на theme из контекста).
export function useThemeColors(): ThemeColors {
  const { theme } = useTheme()
  return useMemo(
    () => ({
      brand: readToken('--brand', '#7045a0'),
      brandSoft: readToken('--brand-soft', '#8a60be'),
      line: readToken('--line', '#d6d2c2'),
      panel: readToken('--bg-panel', '#ffffff'),
      elevated: readToken('--bg-elevated', '#f0ece2'),
      muted: readToken('--muted', '#6e707e'),
      danger: readToken('--danger', '#e12d4c'),
      mask: theme === 'dark' ? 'rgba(10,10,15,0.7)' : 'rgba(191,197,206,0.45)',
    }),
    [theme],
  )
}
