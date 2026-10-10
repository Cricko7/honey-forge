/** @type {import('tailwindcss').Config} */
// Цвета заданы через CSS-переменные (rgb-триплеты) → одна система токенов
// работает для светлой (SPI: Silver/Purple/Ivory + графит) и тёмной тем.
const v = (name) => `rgb(var(${name}) / <alpha-value>)`

export default {
  darkMode: ['selector', '[data-theme="dark"]'],
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        bg: {
          base: v('--bg-base'),
          panel: v('--bg-panel'),
          elevated: v('--bg-elevated'),
          hover: v('--bg-hover'),
        },
        line: v('--line'),
        fg: v('--fg'),
        brand: {
          DEFAULT: v('--brand'),
          soft: v('--brand-soft'),
          deep: v('--brand-deep'),
        },
        // графитовая боковая панель / топбар (тёмные в любой теме)
        graphite: {
          DEFAULT: v('--graphite'),
          soft: v('--graphite-soft'),
          line: v('--graphite-line'),
          fg: v('--graphite-fg'),
          muted: v('--graphite-muted'),
        },
        ok: v('--ok'),
        warn: v('--warn'),
        danger: v('--danger'),
        info: v('--info'),
        muted: v('--muted'),
      },
      fontFamily: {
        sans: ['Inter', 'system-ui', 'Segoe UI', 'sans-serif'],
        mono: ['JetBrains Mono', 'ui-monospace', 'monospace'],
      },
      boxShadow: {
        glass: 'var(--shadow-glass)',
        glow: '0 0 24px rgb(var(--brand) / 0.35)',
      },
      backgroundImage: {
        'grid-fade': 'radial-gradient(circle at 50% 0%, rgb(var(--brand) / 0.12), transparent 60%)',
      },
      keyframes: {
        pulseGlow: {
          '0%, 100%': { opacity: '1' },
          '50%': { opacity: '0.4' },
        },
        slideIn: {
          from: { opacity: '0', transform: 'translateY(8px)' },
          to: { opacity: '1', transform: 'translateY(0)' },
        },
      },
      animation: {
        pulseGlow: 'pulseGlow 2s ease-in-out infinite',
        slideIn: 'slideIn 0.25s ease-out',
      },
    },
  },
  plugins: [],
}
