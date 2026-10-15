import { Moon, Sun } from 'lucide-react'
import { useTheme } from '@/lib/theme'

export function ThemeToggle() {
  const { theme, toggle } = useTheme()
  const dark = theme === 'dark'

  return (
    <button
      onClick={toggle}
      aria-label={dark ? 'Светлая тема' : 'Тёмная тема'}
      title={dark ? 'Светлая тема' : 'Тёмная тема'}
      className="relative h-9 w-9 grid place-items-center overflow-hidden rounded-xl border border-line bg-bg-elevated text-muted transition-[color,background-color,transform] duration-200 hover:text-fg hover:bg-bg-hover active:scale-90"
    >
      <Sun
        size={17}
        className={`col-start-1 row-start-1 transition-all duration-300 ${
          dark ? 'rotate-0 opacity-100 scale-100' : '-rotate-90 opacity-0 scale-50'
        }`}
      />
      <Moon
        size={17}
        className={`col-start-1 row-start-1 transition-all duration-300 ${
          dark ? 'rotate-90 opacity-0 scale-50' : 'rotate-0 opacity-100 scale-100'
        }`}
      />
    </button>
  )
}
