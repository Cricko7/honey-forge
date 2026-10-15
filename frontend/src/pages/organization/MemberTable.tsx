import type { OrganizationMember } from '@/types'
import { formatDateTime } from '@/lib/utils'

export function MemberTable({ members, creatorID, canDelegate, pending, onChange }: {
  members: OrganizationMember[]
  creatorID?: string
  canDelegate: boolean
  pending: boolean
  onChange: (member: OrganizationMember) => void
}) {
  return <div className="overflow-x-auto">
    <table className="data-table w-full min-w-[720px] text-sm">
      <thead><tr className="text-left text-xs text-muted border-b border-line">
        <th className="px-5 py-3 font-medium">Участник</th>
        <th className="px-5 py-3 font-medium">Статус</th>
        <th className="px-5 py-3 font-medium">Управление ловушками</th>
        <th className="px-5 py-3 font-medium">Доступ</th>
      </tr></thead>
      <tbody>{members.map(member => <tr key={member.id} className="border-b border-line/50 last:border-0">
        <td className="px-5 py-4">
          <div className="font-medium break-all">{member.email}</div>
          <div className="text-xs text-muted mt-1">{member.id === creatorID ? 'Создатель организации' : member.role === 'admin' ? 'Администратор' : 'Участник'}</div>
        </td>
        <td className="px-5 py-4 whitespace-nowrap">
          <span className={`inline-flex items-center gap-2 ${member.online ? 'text-ok' : 'text-muted'}`}>
            <span className={`h-2 w-2 rounded-full ${member.online ? 'bg-ok' : 'bg-muted'}`} />
            {member.online ? 'В сети' : 'Не в сети'}
          </span>
          {member.last_seen_at && <div className="text-xs text-muted mt-1">Сигнал: {formatDateTime(member.last_seen_at)}</div>}
        </td>
        <td className="px-5 py-4">
          <span className={`chip ${member.can_manage_traps ? 'bg-brand/10 text-brand' : 'bg-bg-elevated text-muted'}`}>
            {member.role === 'admin' ? 'По роли администратора' : member.can_manage_traps ? 'Делегировано' : 'Только просмотр'}
          </span>
        </td>
        <td className="px-5 py-4 whitespace-nowrap">
          {member.role === 'admin' ? <span className="text-xs text-muted">Права защищены</span> : canDelegate ?
            <button className={member.can_manage_traps ? 'btn-ghost text-danger' : 'btn-ghost text-brand'} disabled={pending} onClick={() => onChange(member)}>
              {member.can_manage_traps ? 'Отозвать право' : 'Выдать право'}
            </button> : <span className="text-xs text-muted">Меняет создатель</span>}
        </td>
      </tr>)}</tbody>
    </table>
    {members.length === 0 && <p className="p-6 text-sm text-muted">Участников нет.</p>}
  </div>
}
