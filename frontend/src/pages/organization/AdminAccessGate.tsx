import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { LockKeyhole } from 'lucide-react'
import { api, USE_MOCK } from '@/api/client'
import { Spinner } from '@/components/ui'
import { AdminPasswordForm } from './AdminPasswordForm'
import type { SessionView } from '@/types'

export function AdminAccessGate({ session, children }: { session: SessionView; children: ReactNode }) {
  const qc = useQueryClient()
  const [blocked, setBlocked] = useState(false)
  const [locking, setLocking] = useState(false)
  const [lockError, setLockError] = useState('')
  const [now, setNow] = useState(Date.now)
  const confirmation = useRef<AbortController | null>(null)
  const key = ['admin-access', session.organization.id, session.user.id]
  const privateKey = ['organization-members', session.organization.id, session.user.id]
  const status = useQuery({ queryKey: key, queryFn: ({ signal }) => api.getAdminAccess(signal),
    retry: false, gcTime: 0, staleTime: 0, refetchInterval: 15_000, refetchOnWindowFocus: 'always' })
  const expires = status.data?.expires_at ? Date.parse(status.data.expires_at) : 0
  const unlocked = !blocked && !status.isError && expires > now

  useEffect(() => () => { confirmation.current?.abort() }, [])
  useEffect(() => {
    setNow(Date.now())
    if (!expires) return
    const timer = window.setTimeout(() => { setNow(Date.now()) }, Math.max(0, expires - Date.now()))
    return () => window.clearTimeout(timer)
  }, [expires])
  useEffect(() => {
    if (unlocked) return
    // Remove private data and cancel its requests when access expires or fails.
    void qc.cancelQueries({ queryKey: privateKey }).then(() => qc.removeQueries({ queryKey: privateKey }))
  }, [unlocked, qc, session.organization.id, session.user.id])

  async function confirm(password: string) {
    confirmation.current?.abort()
    const controller = new AbortController()
    confirmation.current = controller
    const next = await api.confirmAdminAccess(password, controller.signal)
    if (controller.signal.aborted) return
    setNow(Date.now())
    setBlocked(false)
    setLockError('')
    qc.setQueryData(key, next)
    await qc.invalidateQueries({ queryKey: key })
  }

  async function lock() {
    setBlocked(true)
    setLocking(true)
    setLockError('')
    try {
      await api.lockAdminAccess()
      qc.setQueryData(key, { expires_at: null })
    } catch {
      setLockError('Панель скрыта, но сервер не подтвердил блокировку. Повторите попытку.')
    } finally { setLocking(false) }
  }

  if (status.isPending) return <div className="p-8"><Spinner label="Проверка доступа…" /></div>
  if (!unlocked) return <div className="p-6 lg:p-8 space-y-4">
    {status.isError ? <div role="alert" className="card p-6 max-w-lg mx-auto space-y-3">
      <p className="text-sm text-danger">Не удалось проверить доступ к админ-панели.</p>
      <button className="btn-ghost" onClick={() => { void status.refetch() }}>Повторить проверку</button>
    </div> : <AdminPasswordForm disabled={USE_MOCK || locking} onConfirm={confirm} />}
    {lockError && <div role="alert" className="max-w-lg mx-auto text-danger text-sm">
      <p>{lockError}</p><button className="btn-ghost mt-2" disabled={locking} onClick={() => { void lock() }}>Повторить блокировку</button>
    </div>}
    {USE_MOCK && <p className="max-w-lg mx-auto text-xs text-muted">Проверка пароля доступна при подключении к серверу. В демо-режиме не вводите реальные пароли.</p>}
  </div>

  return <>
    <div className="px-6 pt-5 flex flex-wrap items-center justify-between gap-3">
      <span className="text-xs text-muted">Доступ подтверждён до {new Date(expires).toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })}</span>
      <button className="btn-ghost" onClick={() => { void lock() }}><LockKeyhole size={15} /> Заблокировать панель</button>
    </div>
    {children}
  </>
}
