import { Navigate } from 'react-router-dom'
import { useSession } from '@/api/hooks'
import { sessionDestination } from '@/lib/auth'
import { Spinner } from './ui'

export function SessionEntry({ children }: { children?: React.ReactNode }) {
  const { data, isLoading, error, refetch } = useSession()
  const destination = sessionDestination(data, isLoading, error)
  if (destination === 'loading') return <div className="p-8"><Spinner /></div>
  if (destination === 'app') return <Navigate to="/app" replace />
  if (destination === 'error') return (
    <div className="p-8 text-center">
      <p>Не удалось проверить сессию. Проверьте подключение.</p>
      <button className="btn-primary mt-4" onClick={() => refetch()}>Повторить</button>
    </div>
  )
  return children ?? <Navigate to="/login" replace />
}
