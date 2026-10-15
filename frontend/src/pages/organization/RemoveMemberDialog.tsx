import { Modal } from '@/components/Modal'

export function RemoveMemberDialog({ member, pending, error, onClose, onConfirm }: {
  member: { email: string }
  pending: boolean
  error?: string
  onClose: () => void
  onConfirm: () => void
}) {
  return <Modal open title="Удалить участника из организации?" onClose={() => { if (!pending) onClose() }}>
    <div className="space-y-4">
      <p className="text-sm font-medium break-all">{member.email}</p>
      <p className="text-sm text-muted">Участник потеряет доступ к организации. Все его сеансы и API-ключи будут отозваны, повторный вход будет запрещён. История действий и данные организации сохранятся.</p>
      <p className="text-sm text-muted">Отменить удаление через интерфейс нельзя. Уже принятые запросы и команды могут завершиться.</p>
      {error && <p role="alert" className="text-sm text-danger">{error}</p>}
      <div className="flex flex-wrap justify-end gap-3">
        <button autoFocus className="btn-ghost" disabled={pending} onClick={onClose}>Отмена</button>
        <button className="btn-danger" disabled={pending || !!error} onClick={onConfirm}>{pending ? 'Удаление…' : 'Удалить из организации'}</button>
      </div>
    </div>
  </Modal>
}
