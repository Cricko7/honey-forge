import { useState, type FormEvent } from 'react'
import { ApiError } from '@/api/http'

export function AdminPasswordForm({ onConfirm, disabled = false }: {
  onConfirm: (password: string) => Promise<void>
  disabled?: boolean
}) {
  const [password, setPassword] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (pending || disabled) return
    const entered = password
    setPassword('')
    setError('')
    setPending(true)
    try { await onConfirm(entered) }
    catch (err) {
      setError(err instanceof ApiError && err.status === 401 ? 'Неверный пароль аккаунта. Повторите ввод.' :
        err instanceof ApiError && err.status === 429 ? 'Слишком много попыток. Подождите перед повторным вводом.' :
        'Не удалось подтвердить доступ. Проверьте соединение и повторите попытку.')
    } finally { setPending(false) }
  }
  return <section className="border-t border-line pt-5 max-w-lg mx-auto space-y-4">
    <h1 className="text-xl font-semibold">Подтвердите доступ к админ-панели</h1>
    <p className="text-sm text-muted">Повторно введите пароль своего аккаунта. Доступ откроется на 15 минут для текущей браузерной сессии.</p>
    <form onSubmit={submit} className="space-y-4">
      <label className="block text-sm font-medium">Пароль аккаунта
        <input autoFocus type="password" autoComplete="current-password" name="password" required minLength={12} maxLength={128}
          value={password} onChange={event => setPassword(event.target.value)} disabled={pending || disabled}
          className="input w-full mt-2" />
      </label>
      {error && <p role="alert" className="text-sm text-danger">{error}</p>}
      <button className="btn-primary w-full" type="submit" disabled={pending || disabled}>{pending ? 'Проверка пароля…' : 'Открыть админ-панель'}</button>
    </form>
    <p className="text-xs text-muted">Пароль не сохраняется приложением. После выхода из аккаунта потребуется новое подтверждение.</p>
  </section>
}
