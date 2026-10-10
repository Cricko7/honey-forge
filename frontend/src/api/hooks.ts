import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import type { CommandAction, EventSummary, Trap } from '@/types'
import { api, subscribeStream } from './client'

export const qk = {
  session: ['session'] as const,
  catalog: ['catalog'] as const,
  profiles: ['profiles'] as const,
  traps: (f?: object) => ['traps', f ?? {}] as const,
  trap: (id: string) => ['trap', id] as const,
  commands: (trapId: string) => ['commands', trapId] as const,
  events: (f?: object) => ['events', f ?? {}] as const,
  event: (id: string) => ['event', id] as const,
}

export const useSession = () => useQuery({ queryKey: qk.session, queryFn: api.getSession })
export const useCatalog = () => useQuery({ queryKey: qk.catalog, queryFn: api.listCatalog })
export const useProfiles = () => useQuery({ queryKey: qk.profiles, queryFn: api.listProfiles })

export const useTraps = (filter?: { connectivity?: string; profile_id?: string }) =>
  useQuery({ queryKey: qk.traps(filter), queryFn: () => api.listTraps(filter) })

export const useTrap = (id: string) =>
  useQuery({ queryKey: qk.trap(id), queryFn: () => api.getTrap(id), enabled: !!id })

export const useCommands = (trapId: string) =>
  useQuery({ queryKey: qk.commands(trapId), queryFn: () => api.listCommands(trapId), enabled: !!trapId })

export const useEvents = (filter?: { trap_id?: string; limit?: number }) =>
  useQuery({ queryKey: qk.events(filter), queryFn: () => api.listEvents(filter) })

export const useEvent = (id: string) =>
  useQuery({ queryKey: qk.event(id), queryFn: () => api.getEvent(id), enabled: !!id })

export function useCreateProfile() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: api.createProfile,
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.profiles }),
  })
}

export function useCreateTrap() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: api.createTrap,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['traps'] }),
  })
}

export function useDeleteTrap() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, revision }: { id: string; revision: number }) => api.deleteTrap(id, revision),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['traps'] }),
  })
}

export function useSendCommand() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ trapId, action }: { trapId: string; action: CommandAction }) =>
      api.createCommand(trapId, action),
    onSuccess: (_d, v) => {
      qc.invalidateQueries({ queryKey: qk.commands(v.trapId) })
    },
  })
}

// Подписка на live-стрим: пушит новые события/изменения ловушек прямо в кэш
// React Query, чтобы дашборд и конструктор обновлялись в реальном времени.
export function useLiveStream(onEvent?: (e: EventSummary) => void) {
  const qc = useQueryClient()
  useEffect(() => {
    return subscribeStream((n) => {
      if (n.kind === 'event.created') {
        qc.setQueriesData<{ items: EventSummary[]; next_cursor: string | null }>(
          { queryKey: ['events'] },
          (old) => (old ? { ...old, items: [n.event, ...old.items].slice(0, 200) } : old),
        )
        onEvent?.(n.event)
      } else if (n.kind === 'trap.changed') {
        qc.setQueriesData<{ items: Trap[]; next_cursor: string | null }>({ queryKey: ['traps'] }, (old) => {
          if (!old) return old
          const exists = old.items.some((t) => t.id === n.trap.id)
          const items = exists
            ? old.items.map((t) => (t.id === n.trap.id ? n.trap : t))
            : [n.trap, ...old.items]
          return { ...old, items }
        })
        qc.setQueryData(qk.trap(n.trap.id), n.trap)
      } else if (n.kind === 'trap.deleted') {
        qc.setQueriesData<{ items: Trap[]; next_cursor: string | null }>({ queryKey: ['traps'] }, (old) =>
          old ? { ...old, items: old.items.filter((t) => t.id !== n.trap_id) } : old,
        )
      } else if (n.kind === 'command.changed') {
        qc.invalidateQueries({ queryKey: qk.commands(n.trap_id) })
      }
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
}
