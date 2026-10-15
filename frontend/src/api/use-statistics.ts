import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, ApiError } from './client'
import { useSessionClient } from './session-scope'
import { qk } from './query-keys'
import { loadStatisticsSample } from './statistics'

export function useStatistics(size: number) {
  const sessionClient = useSessionClient()
  const query = useQuery({
    queryKey: ['statistics', size],
    queryFn: ({ signal }) => loadStatisticsSample(api.listEvents, size, signal),
    // Keep a coherent snapshot until the operator explicitly refreshes it.
    staleTime: Infinity, gcTime: 0, retry: false,
    refetchOnWindowFocus: false, refetchOnReconnect: false,
  })
  useEffect(() => {
    if (query.error instanceof ApiError && [401, 403].includes(query.error.status)) {
      void sessionClient.invalidateQueries({ queryKey: qk.session })
    }
  }, [query.error, sessionClient])
  return query
}
