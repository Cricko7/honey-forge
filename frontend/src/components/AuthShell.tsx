import type { ReactNode } from 'react'
import { Logo } from './Logo'
import { ThemeToggle } from './ThemeToggle'

export function AuthShell({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  return <div className="min-h-dvh flex flex-col">
    <header className="flex items-center justify-between px-6 py-4 border-b border-line bg-bg-panel">
      <div className="flex items-center gap-2.5 font-semibold"><Logo size={28} /> HoneyForge</div>
      <ThemeToggle />
    </header>
    <main className="flex-1 flex items-center justify-center px-5 py-10">
      <div className="w-full max-w-sm">
        <h1 id="auth-title" className="text-2xl font-semibold tracking-tight">{title}</h1>
        <p className="mt-2 mb-7 text-sm leading-relaxed text-muted">{description}</p>
        {children}
      </div>
    </main>
  </div>
}
