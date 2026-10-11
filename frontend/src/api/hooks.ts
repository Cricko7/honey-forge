import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import type { CommandAction, EventSummary } from '@/types'
import { api, subscribeStream } from './client'
import { applyStreamNotification, removeDeletedTrap } from './cache'

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
  useQuery({ queryKey: qk.trap(id), queryFn: () => api.getTrap(id), enabled: !!id, refetchInterval: 3000 })

export const useCommands = (trapId: string) =>
  useQuery({
    queryKey: qk.commands(trapId),
    queryFn: () => api.listCommands(trapId),
    enabled: !!trapId,
    refetchInterval: (query) => query.state.data?.items.some((c) => c.status === 'queued' || c.status === 'running') ? 3000 : false,
  })

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
    onSuccess: async (_data, { id }) => {
      await Promise.all([
        qc.cancelQueries({ queryKey: ['traps'] }),
        qc.cancelQueries({ queryKey: qk.trap(id), exact: true }),
      ])
      removeDeletedTrap(qc, id)
      await qc.invalidateQueries({ queryKey: ['traps'] })
    },
  })
}

export function useSendCommand() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({
      trapId,
      action,
      params,
    }: {
      trapId: string
      action: CommandAction
      params?: Record<string, unknown>
    }) => api.createCommand(trapId, action, params),
    onSuccess: (_d, v) => {
      qc.invalidateQueries({ queryKey: qk.commands(v.trapId) })
      qc.invalidateQueries({ queryKey: qk.trap(v.trapId) })
    },
  })
}

// Подписка на live-стрим: пушит новые события/изменения ловушек прямо в кэш
// React Query, чтобы дашборд и конструктор обновлялись в реальном времени.
export function useLiveStream(onEvent?: (e: EventSummary) => void) {
  const qc = useQueryClient()
  useEffect(() => {
    return subscribeStream((n) => {
      applyStreamNotification(qc, n)
      if (n.kind === 'event.created') onEvent?.(n.event)
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
}
