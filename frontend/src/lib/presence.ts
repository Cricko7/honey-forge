import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '@/api/client'
import { qk } from '@/api/query-keys'

// Only a visible authenticated application reports presence. Closing or hiding
// every tab expires presence on the server without an unreliable unload request.
export function usePresence(userID: string) {
  const qc = useQueryClient()
  useEffect(() => {
    const controller = new AbortController()
    let pending = false
    async function report() {
      if (document.visibilityState !== 'visible' || pending || controller.signal.aborted) return
      pending = true
      try { await api.sendPresence(controller.signal) }
      catch (err) {
        if (!controller.signal.aborted && err instanceof ApiError && err.status === 401) {
          await qc.invalidateQueries({ queryKey: qk.session })
        }
      } finally { pending = false }
    }
    void report()
    const timer = window.setInterval(() => { void report() }, 30_000)
    const onVisibility = () => { void report() }
    document.addEventListener('visibilitychange', onVisibility)
    return () => {
      controller.abort()
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', onVisibility)
    }
  }, [userID, qc])
}
