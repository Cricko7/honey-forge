import type { MutationCache } from '@tanstack/react-query'
import { actionErrorMessage, type AlertInput } from '../lib/alerts.ts'

export function subscribeMutationAlerts(cache: MutationCache, notify: (alert: AlertInput) => void) {
  const owned = new WeakSet<object>()
  return cache.subscribe((event) => {
    if (event.type === 'added') { owned.add(event.mutation); return }
    if (event.type === 'removed') { owned.delete(event.mutation); return }
    if (event.type !== 'updated' || !owned.has(event.mutation)) return
    const { mutation, action } = event
    const meta = mutation.options.meta
    if (action.type === 'success' && typeof meta?.alertSuccess === 'string') {
      const variables = mutation.state.variables
      const deletedId = meta.alertTrapDeletion && variables && typeof variables === 'object'
        && 'id' in variables && typeof variables.id === 'string' ? variables.id : undefined
      notify({ id: deletedId ? `deleted:${deletedId}` : `mutation:${mutation.mutationId}:success`, title: meta.alertSuccess, level: 'success' })
    } else if (action.type === 'error' && typeof meta?.alertError === 'string') {
      notify({ id: `mutation:${mutation.mutationId}:error`, title: meta.alertError,
        message: actionErrorMessage(mutation.state.error), level: 'error' })
    }
  })
}
