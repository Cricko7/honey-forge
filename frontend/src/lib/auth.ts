import type { SessionView } from '@/types'

export type RegistrationMode = 'create' | 'join'

export function registrationOrganization(mode: RegistrationMode, value: string) {
  return mode === 'join'
    ? { mode, join_code: value.trim() }
    : { mode, name: value.trim() }
}

export function canManage(session: SessionView | null | undefined): boolean {
  return session?.user.role === 'admin'
}

export function canManageTraps(session: SessionView | null | undefined): boolean {
  return canManage(session) || (session?.user.role === 'viewer' && session.user.can_manage_traps === true)
}

export function sessionDestination(
  session: SessionView | null | undefined,
  loading: boolean,
  error: unknown,
): 'loading' | 'app' | 'guest' | 'error' {
  if (loading) return 'loading'
  if (error) return (error as { status?: number }).status === 401 ? 'guest' : 'error'
  return session ? 'app' : 'guest'
}
