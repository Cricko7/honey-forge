import type { Node } from 'reactflow'

const ACTIVITY_MS = 2500

// Publish only changes to the active set; incoming traffic just extends its deadline.
export function createNetworkActivity(onChange: (active: Record<string, true>) => void) {
  const deadlines = new Map<string, number>()
  let timer: ReturnType<typeof setTimeout> | undefined
  let disposed = false
  const publish = () => onChange(Object.fromEntries([...deadlines.keys()].map((id) => [id, true])))
  const schedule = () => {
    if (timer !== undefined || deadlines.size === 0) return
    let next = Infinity
    for (const deadline of deadlines.values()) next = Math.min(next, deadline)
    timer = setTimeout(expire, Math.max(0, next - Date.now()))
  }
  function expire() {
    timer = undefined
    const now = Date.now()
    let changed = false
    for (const [id, deadline] of deadlines) {
      if (deadline <= now) {
        deadlines.delete(id)
        changed = true
      }
    }
    if (changed) publish()
    schedule()
  }
  return {
    accept(id: string) {
      if (disposed) return
      const active = deadlines.has(id)
      deadlines.set(id, Date.now() + ACTIVITY_MS)
      if (!active) publish()
      schedule()
    },
    dispose() {
      disposed = true
      clearTimeout(timer)
      deadlines.clear()
    },
  }
}

export function reconcileNetworkNodes<T>(current: Node<T>[], fresh: Node<T>[], resetPositions = false): Node<T>[] {
  const byId = new Map(current.map((node) => [node.id, node]))
  return fresh.map((node) => {
    const previous = byId.get(node.id)
    return previous ? { ...previous, ...node, position: resetPositions ? node.position : previous.position } : node
  })
}
