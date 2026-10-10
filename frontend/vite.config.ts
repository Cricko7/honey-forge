import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { fileURLToPath, URL } from 'node:url'

// Фронтенд HoneyForge. Dev-сервер на 3000 — совпадает с BROWSER_ORIGINS
// (https://localhost:3000) из backend README. Прокси на реальный API
// включается через VITE_API_BASE; по умолчанию работает mock-слой.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 3000,
    host: '127.0.0.1',
  },
})
