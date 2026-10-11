import type { StreamNotification } from '../types'

interface StreamOptions {
  synchronize: () => Promise<string | null>
  checkSession: () => Promise<unknown>
  onUnauthorized: () => void
}

// Cursors belong to this subscription only; never reuse them across accounts.
export function subscribeDashboardStream(base: string, onNotify: (n: StreamNotification) => void, options: StreamOptions): () => void {
  const url = new URL(`${base.replace(/\/+$/, '')}/api/stream`, window.location.origin)
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
  let stopped = false
  let socket: WebSocket | undefined
  let timer: ReturnType<typeof setTimeout> | undefined
  let cursor: string | null = null
  let synchronize = true
  let attempts = 0
  let slowCloses = 0

  const retry = () => {
    if (stopped) return
    timer = setTimeout(() => { void connect() }, Math.min(1000 * 2 ** attempts++, 30000))
  }
  const unauthorized = () => {
    stopped = true
    options.onUnauthorized()
  }
  const connect = async () => {
    if (stopped) return
    try {
      // Browsers hide HTTP handshake errors. REST distinguishes a lost session
      // from a transient outage and avoids retrying an invalid cookie forever.
      await options.checkSession()
      if (stopped) return
      if (synchronize) {
        cursor = await options.synchronize()
        synchronize = false
      }
      if (stopped) return
      const ws = new WebSocket(url.href, 'dashboard-stream.v1')
      socket = ws
      let resetCursor = false
      ws.onopen = () => {
        if (stopped) { ws.close(); return }
        ws.send(JSON.stringify({ message_id: crypto.randomUUID(), type: 'stream.subscribe', payload: { after: cursor } }))
      }
      ws.onmessage = (ev) => {
        if (stopped || ws !== socket) return
        let env
        try { env = JSON.parse(ev.data) } catch { return }
        if (!env || typeof env !== 'object') return
        if (env.type === 'error') {
          resetCursor = ['cursor_expired', 'invalid_cursor'].includes(env.payload?.error?.code)
          return
        }
        const data = env.payload?.data
        try {
          if (env.type === 'event.created' && data?.event) onNotify({ kind: 'event.created', event: data.event })
          else if (env.type === 'trap.changed' && data?.trap) onNotify({ kind: 'trap.changed', trap: data.trap })
          else if (env.type === 'trap.deleted' && data?.trap_id) onNotify({ kind: 'trap.deleted', trap_id: data.trap_id })
          else if (env.type === 'command.changed' && data?.command)
            onNotify({ kind: 'command.changed', trap_id: data.trap_id, command: data.command })
        } catch {
          // Retry from the last applied cursor if the consumer failed.
          ws.close()
          return
        }
        if (typeof env.payload?.cursor === 'string') cursor = env.payload.cursor
        if (env.type === 'stream.ready') attempts = 0
      }
      ws.onclose = (ev) => {
        if (stopped || ws !== socket) return
        if (ev.code === 4401 || ev.code === 4403) { unauthorized(); return }
        if (resetCursor || (ev.code === 4410 && ++slowCloses > 1)) synchronize = true
        // Unsupported frames/size and other protocol errors require a code fix.
        else if ([1003, 1009, 4400].includes(ev.code)) return
        retry()
      }
    } catch (err) {
      if (stopped) return
      if (err && typeof err === 'object' && 'status' in err && (err.status === 401 || err.status === 403)) unauthorized()
      else retry()
    }
  }
  // React StrictMode runs setup/cleanup/setup synchronously. Deferring creation
  // avoids closing a socket whose first handshake hasn't finished yet.
  queueMicrotask(() => { void connect() })
  return () => {
    stopped = true
    clearTimeout(timer)
    if (!socket) return
    socket.onmessage = null
    socket.onclose = null
    if (socket.readyState === WebSocket.CONNECTING) socket.onopen = () => socket?.close()
    else if (socket.readyState === WebSocket.OPEN) socket.close()
  }
}
