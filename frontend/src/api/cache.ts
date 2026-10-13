import type { QueryClient } from '@tanstack/react-query'
import { qk } from './query-keys.ts'
import { matchesEvent, type EventFilter } from './event-filters.ts'
import type { EventSummary, Page, Profile, SessionView, StreamNotification, Trap } from '../types'

const deletedTraps = new WeakMap<QueryClient, Set<string>>()

export async function replaceSessionCache(qc: QueryClient, session?: SessionView) {
  await qc.cancelQueries()
  qc.clear()
  deletedTraps.delete(qc)
  qc.setQueryData(qk.session, session ?? null)
}

export function removeDeletedTrap(qc: QueryClient, id: string) {
  let deleted = deletedTraps.get(qc)
  if (!deleted) { deleted = new Set(); deletedTraps.set(qc, deleted) }
  deleted.add(id)
  qc.setQueriesData<Page<Trap>>({ queryKey: qk.trapLists }, (old) =>
    old ? { ...old, items: old.items.filter((t) => t.id !== id) } : old,
  )
  qc.setQueryData(qk.trap(id), null)
}

export function applyStreamNotification(qc: QueryClient, n: StreamNotification) {
  if (n.kind === 'event.created') {
    for (const [key] of qc.getQueriesData({ queryKey: qk.eventLists })) {
      const filter = key[1] as EventFilter | undefined
      if (filter?.cursor || !matchesEvent(n.event, filter)) continue
      const cached = qc.getQueryData<Page<EventSummary>>(key)
      if (cached?.next_cursor) {
        // Refetch items and cursor together so live inserts cannot skip the last item.
        qc.invalidateQueries({ queryKey: key, exact: true })
        continue
      }
      qc.setQueryData<Page<EventSummary>>(key, (old) => old ? {
        ...old,
        items: [n.event, ...old.items.filter((e) => e.event_id !== n.event.event_id)]
          .slice(0, filter?.limit ?? 50),
      } : old)
    }
  } else if (n.kind === 'trap.changed') {
    if (deletedTraps.get(qc)?.has(n.trap.id)) return
    const detail = qc.getQueryData<Trap>(qk.trap(n.trap.id))
    const snapshots = [detail, ...qc.getQueriesData<Page<Trap>>({ queryKey: qk.trapLists })
      .flatMap(([, cached]) => cached?.items.filter((t) => t.id === n.trap.id) ?? [])]
    if (snapshots.some((t) => t && t.state_version >= n.trap.state_version)) return
    for (const [key] of qc.getQueriesData({ queryKey: qk.trapLists })) {
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
    qc.setQueryData(qk.trap(n.trap.id), n.trap)
  } else if (n.kind === 'trap.deleted') {
    removeDeletedTrap(qc, n.trap_id)
  } else if (n.kind === 'profile.deleted') {
    qc.setQueryData<Page<Profile>>(qk.profiles, (old) =>
      old ? { ...old, items: old.items.filter((p) => p.id !== n.profile_id) } : old,
    )
    qc.invalidateQueries({ queryKey: qk.profiles })
  } else if (n.kind === 'command.changed') {
    qc.invalidateQueries({ queryKey: qk.commands(n.trap_id) })
    qc.invalidateQueries({ queryKey: qk.trap(n.trap_id), exact: true })
  }
}
