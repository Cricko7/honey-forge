import { Moon, Sun } from 'lucide-react'
import { useTheme } from '@/lib/theme'

// Минималистичный переключатель темы: иконка-кнопка солнце/луна.
export function ThemeToggle() {
  const { theme, toggle } = useTheme()
  const dark = theme === 'dark'

  return (
    <button
      onClick={toggle}
      aria-label={dark ? 'Светлая тема' : 'Тёмная тема'}
      title={dark ? 'Светлая тема' : 'Тёмная тема'}
      className="h-9 w-9 grid place-items-center rounded-xl border border-line bg-bg-elevated text-muted hover:text-fg hover:bg-bg-hover transition-colors"
    >
      {dark ? <Sun size={17} /> : <Moon size={17} />}
    </button>
  )
}
