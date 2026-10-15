import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import { QueryClient, QueryClientProvider, useQueryClient } from '@tanstack/react-query'

const SessionClient = createContext<QueryClient | null>(null)

// Authentication has one cache outside the account's disposable resource cache.
export function useSessionClient() {
  const fallback = useQueryClient()
  return useContext(SessionClient) ?? fallback
}

export function createSessionCache(sessionClient: QueryClient) {
  return new QueryClient({ defaultOptions: sessionClient.getDefaultOptions() })
}

export async function disposeSessionCache(client: QueryClient) {
  await client.cancelQueries()
  client.clear()
}

// The parent keys this scope by user, organization and browser session. Old callbacks
// retain the old client, so they cannot repopulate the next account's cache.
export function SessionScope({ sessionClient, children }: { sessionClient: QueryClient; children: ReactNode }) {
  const [client] = useState(() => createSessionCache(sessionClient))
  const generation = useRef(0)
  useEffect(() => {
    const current = ++generation.current
    return () => {
      // React StrictMode immediately mounts the same scope again in development.
      queueMicrotask(() => {
        if (generation.current === current) void disposeSessionCache(client)
      })
    }
  }, [client])
  return <SessionClient.Provider value={sessionClient}>
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  </SessionClient.Provider>
}
