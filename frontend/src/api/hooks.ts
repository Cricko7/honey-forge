import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef } from 'react'
import type { CommandAction, EventSummary, StreamNotification } from '@/types'
import type { EventFilter } from './event-filters'
import { api, subscribeStream } from './client'
import { qk } from './query-keys'
import { canManage, canManageTraps } from '@/lib/auth'
import { applyStreamNotification, removeDeletedTrap } from './cache'

export { qk } from './query-keys'

export const useSession = () => useQuery({ queryKey: qk.session, queryFn: api.getSession, refetchInterval: 30_000, refetchOnWindowFocus: true })
export const useCanManage = () => canManage(useSession().data)
export const useCanManageTraps = () => canManageTraps(useSession().data)
export const useDeploymentNodes = () => useQuery({ queryKey: ["deployment-nodes"], queryFn: api.listDeploymentNodes, refetchInterval: 5000 })
export const useCatalog = () => useQuery({ queryKey: qk.catalog, queryFn: api.listCatalog })
export const useProfiles = () => useQuery({ queryKey: qk.profiles, queryFn: api.listProfiles })
export const useTrapLinks = () => useQuery({ queryKey: qk.trapLinks, queryFn: api.listTrapLinks, refetchInterval: 3000 })

export function useCreateTrapLink() {
  const qc = useQueryClient()
  return useMutation({ mutationFn: api.createTrapLink,
    meta: { alertSuccess: 'Связь создана', alertError: 'Не удалось создать связь' },
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.trapLinks }) })
}

export function useDeleteTrapLink() {
  const qc = useQueryClient()
  return useMutation({ mutationFn: api.deleteTrapLink,
    meta: { alertSuccess: 'Связь удалена', alertError: 'Не удалось удалить связь' },
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.trapLinks }) })
}

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

export const useEvents = (filter?: EventFilter, enabled = true) =>
  useQuery({ queryKey: qk.events(filter), queryFn: () => api.listEvents(filter), enabled })

export const useEvent = (id: string) =>
  useQuery({ queryKey: qk.event(id), queryFn: () => api.getEvent(id), enabled: !!id })

export function useCreateProfile() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: api.createProfile,
    meta: { alertSuccess: 'Профиль создан', alertError: 'Не удалось создать профиль' },
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.profiles }),
  })
}

export function useCreateTrap() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: api.createTrap,
    meta: { alertSuccess: 'Ловушка создана', alertError: 'Не удалось создать ловушку' },
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.trapLists }),
  })
}

export function useDeleteTrap() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, revision }: { id: string; revision: number }) => api.deleteTrap(id, revision),
    meta: { alertSuccess: 'Ловушка удалена', alertError: 'Не удалось удалить ловушку', alertTrapDeletion: true },
    onSuccess: async (_data, { id }) => {
      await Promise.all([
        qc.cancelQueries({ queryKey: qk.trapLists }),
        qc.cancelQueries({ queryKey: qk.trap(id), exact: true }),
      ])
      removeDeletedTrap(qc, id)
      await Promise.all([
        qc.invalidateQueries({ queryKey: qk.trapLists }),
        qc.invalidateQueries({ queryKey: qk.profiles }),
      ])
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
    meta: { alertSuccess: 'Команда отправлена', alertError: 'Не удалось отправить команду' },
    onSuccess: (_d, v) => {
      qc.invalidateQueries({ queryKey: qk.commands(v.trapId) })
      qc.invalidateQueries({ queryKey: qk.trap(v.trapId) })
    },
  })
}

export function useLiveStream(onEvent?: (e: EventSummary) => void, onNotification?: (n: StreamNotification) => void) {
  const qc = useQueryClient()
  const { data: session } = useSession()
  const callbacks = useRef({ onEvent, onNotification })
  callbacks.current = { onEvent, onNotification }
  useEffect(() => {
    if (!session) return
    return subscribeStream((n) => {
      applyStreamNotification(qc, n)
      callbacks.current.onNotification?.(n)
      if (n.kind === 'event.created') callbacks.current.onEvent?.(n.event)
    }, {
      onSynchronize: () => qc.invalidateQueries({
        predicate: (q) => ['events', 'traps', 'trap', 'commands', 'profiles', 'catalog'].includes(String(q.queryKey[0])),
      }),
      onUnauthorized: () => { void qc.invalidateQueries({ queryKey: qk.session }) },
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [qc, session?.user.id, session?.organization.id])
}
