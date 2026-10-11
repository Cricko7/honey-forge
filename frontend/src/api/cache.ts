import type { QueryClient } from '@tanstack/react-query'
import type { EventSummary, Page, SessionView, StreamNotification, Trap } from '../types'

export async function replaceSessionCache(qc: QueryClient, session?: SessionView) {
  await qc.cancelQueries()
  qc.clear()
  qc.setQueryData(['session'], session ?? null)
}

export function removeDeletedTrap(qc: QueryClient, id: string) {
  qc.setQueriesData<Page<Trap>>({ queryKey: ['traps'] }, (old) =>
    old ? { ...old, items: old.items.filter((t) => t.id !== id) } : old,
  )
  qc.setQueryData(['trap', id], null)
}

export function applyStreamNotification(qc: QueryClient, n: StreamNotification) {
  if (n.kind === 'event.created') {
    for (const [key] of qc.getQueriesData({ queryKey: ['events'] })) {
      const filter = key[1] as { trap_id?: string; limit?: number } | undefined
      if (filter?.trap_id && filter.trap_id !== n.event.trap_id) continue
      qc.setQueryData<Page<EventSummary>>(key, (old) => old ? {
        ...old,
        items: [n.event, ...old.items.filter((e) => e.event_id !== n.event.event_id)]
          .slice(0, filter?.limit ?? 50),
      } : old)
    }
  } else if (n.kind === 'trap.changed') {
    for (const [key] of qc.getQueriesData({ queryKey: ['traps'] })) {
      const filter = key[1] as { connectivity?: string; profile_id?: string } | undefined
      const matches = (!filter?.connectivity || filter.connectivity === n.trap.connectivity)
        && (!filter?.profile_id || filter.profile_id === n.trap.profile_id)
      qc.setQueryData<Page<Trap>>(key, (old) => {
        if (!old) return old
        if (!matches) return { ...old, items: old.items.filter((t) => t.id !== n.trap.id) }
        const exists = old.items.some((t) => t.id === n.trap.id)
        return { ...old, items: exists
          ? old.items.map((t) => t.id === n.trap.id ? n.trap : t)
          : [n.trap, ...old.items] }
      })
    }
    qc.setQueryData(['trap', n.trap.id], n.trap)
  } else if (n.kind === 'trap.deleted') {
    removeDeletedTrap(qc, n.trap_id)
  } else if (n.kind === 'command.changed') {
    qc.invalidateQueries({ queryKey: ['commands', n.trap_id] })
    qc.invalidateQueries({ queryKey: ['trap', n.trap_id], exact: true })
  }
}
