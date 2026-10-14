import { useState } from 'react'
import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, Navigate } from 'react-router-dom'
import { ShieldCheck, Users, RefreshCw, Crown } from 'lucide-react'
import { api, ApiError, USE_MOCK } from '@/api/client'
import { useSession } from '@/api/hooks'
import { canManage } from '@/lib/auth'
import { Spinner } from '@/components/ui'
import { MemberTable } from './organization/MemberTable'
import type { OrganizationMember } from '@/types'

export function AdminPage() {
  const { data: session } = useSession()
  const allowed = canManage(session)
  const qc = useQueryClient()
  const [selected, setSelected] = useState<OrganizationMember | null>(null)
  const key = ['organization-members', session?.organization.id, session?.user.id]
  const members = useInfiniteQuery({
    queryKey: key,
    queryFn: ({ pageParam }) => api.listMembers(pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: page => page.next_cursor ?? undefined,
    enabled: allowed, retry: false, gcTime: 0,
    refetchInterval: 15_000, refetchOnWindowFocus: true,
  })
  const creator = members.data?.pages[0].creator
  const canDelegate = !USE_MOCK && !!creator && creator.id === session?.user.id
  const mutation = useMutation({
    mutationFn: (member: OrganizationMember) => api.setTrapPermission(member.id, !member.can_manage_traps, member.permission_revision),
    meta: { alertSuccess: 'Право управления ловушками изменено', alertError: 'Не удалось изменить право управления' },
    onSuccess: async () => {
      setSelected(null)
      await qc.invalidateQueries({ queryKey: key })
    },
    onError: async () => { await qc.invalidateQueries({ queryKey: key }) },
  })
  if (!allowed) return <Navigate to="/app" replace />
  const items = members.data?.pages.flatMap(page => page.items) ?? []
  const uniqueMembers = [...new Map(items.map(member => [member.id, member])).values()]

  return <div className="p-6 lg:p-8 max-w-6xl mx-auto space-y-6">
    <header>
      <span className="chip bg-brand/10 text-brand mb-3"><ShieldCheck size={14} /> Администрирование</span>
      <h1 className="text-2xl font-semibold tracking-tight">Команда организации</h1>
      <p className="text-sm text-muted mt-2">{session?.organization.name} · участники, активность и права управления ловушками</p>
    </header>
    <div className="grid md:grid-cols-2 gap-4">
      <section className="card p-5 flex items-start gap-3">
        <Crown size={22} className="text-brand shrink-0" />
        <div><h2 className="text-sm font-semibold">Создатель организации</h2>
          <p className="mt-1 text-sm break-all">{members.isError ? 'Не удалось проверить' : creator?.email ?? (members.isPending ? 'Загрузка…' : 'Не установлен в исторических данных')}</p>
          <p className="mt-2 text-xs text-muted">Только создатель выдаёт и отзывает делегированное право. Права администраторов защищены.</p>
        </div>
      </section>
      <section className="card p-5 flex items-start gap-3">
        <ShieldCheck size={22} className="text-brand shrink-0" />
        <div><h2 className="text-sm font-semibold">Область делегирования</h2>
          <p className="mt-1 text-xs text-muted leading-relaxed">Создание, изменение и удаление ловушек; запуск, остановка и применение готовой конфигурации. Доступ ко всем ловушкам своей организации.</p>
          <p className="mt-2 text-xs text-muted">API-ключи, приглашения, подключение серверов, изменение профилей и учётные данные агентов остаются у администраторов.</p>
        </div>
      </section>
    </div>
    <section className="card !p-0 overflow-hidden">
      <div className="p-5 border-b border-line flex flex-wrap justify-between gap-3 items-center">
        <h2 className="font-semibold flex items-center gap-2"><Users size={18} className="text-brand" /> Участники</h2>
        <div className="flex gap-3 items-center">
          <Link className="text-sm text-brand" to="/app/organization">Приглашения и API-ключи</Link>
          <button className="btn-ghost" disabled={members.isFetching || mutation.isPending} onClick={() => { void members.refetch() }}><RefreshCw size={15} /> Обновить</button>
        </div>
      </div>
      {members.isPending ? <div className="p-8"><Spinner /></div> : members.isError ?
        <p role="alert" className="p-6 text-sm text-danger">Не удалось проверить состав команды и права. Обновите данные.</p> :
        <MemberTable members={uniqueMembers} creatorID={creator?.id} canDelegate={canDelegate} pending={mutation.isPending || members.isFetching} onChange={member => { mutation.reset(); setSelected(member) }} />}
      {members.hasNextPage && !members.isError && <div className="p-4 border-t border-line"><button className="btn-ghost" disabled={members.isFetching || mutation.isPending} onClick={() => { void members.fetchNextPage() }}>Загрузить ещё</button></div>}
    </section>
    <p className="text-xs text-muted leading-relaxed">«В сети» — видимая вкладка платформы подтверждала активность в последние 90 секунд. Обновление списка каждые 15 секунд. При закрытии вкладок статус изменится после истечения этого срока; выход завершает активность текущей сессии.</p>
    {USE_MOCK && <p className="text-sm text-warn">Демонстрационные данные. Для выдачи прав подключите сервер.</p>}
    {selected && <div className="fixed inset-0 z-50 bg-black/60 grid place-items-center p-4">
      <section role="dialog" aria-modal="true" aria-labelledby="permission-title" className="card p-6 max-w-lg w-full space-y-4">
        <h2 id="permission-title" className="text-lg font-semibold">{selected.can_manage_traps ? 'Отозвать' : 'Выдать'} право управления ловушками?</h2>
        <p className="text-sm break-all">{selected.email}</p>
        <p className="text-sm text-muted">{selected.can_manage_traps ? 'Следующие управляющие запросы будут запрещены. Уже принятые команды могут завершиться. Просмотр данных сохранится.' : 'Участник сможет создавать, изменять и удалять ловушки, запускать и останавливать их, применять готовую конфигурацию во всей организации.'}</p>
        {mutation.isError && <p role="alert" className="text-sm text-danger">{mutation.error instanceof ApiError && mutation.error.code === 'permission_changed' ? 'Права уже изменились. Закройте окно и проверьте актуальные данные.' : 'Изменение не подтверждено. Обновите список перед повторной попыткой.'}</p>}
        <div className="flex justify-end gap-3">
          <button autoFocus className="btn-ghost" disabled={mutation.isPending} onClick={() => setSelected(null)}>Отмена</button>
          <button className={selected.can_manage_traps ? 'btn-danger' : 'btn-primary'} disabled={!canDelegate || members.isError || mutation.isPending || mutation.isError} onClick={() => mutation.mutate(selected)}>{mutation.isPending ? 'Сохранение…' : 'Подтвердить'}</button>
        </div>
      </section>
    </div>}
  </div>
}
