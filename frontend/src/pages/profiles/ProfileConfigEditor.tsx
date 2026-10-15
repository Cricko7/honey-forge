import { useId } from 'react'
import { parseProfileConfig } from '@/lib/profile-config'
import { profileConfigIssues, profileEndpoint, updateEndpoint } from '@/lib/profile-editor'
import { serviceProtocols } from '@/lib/service-protocols'

export function ProfileConfigEditor({ typeId, value, onChange }: {
  typeId: string; value: string; onChange: (value: string) => void
}) {
  const id = useId()
  let config: Record<string, unknown> = {}
  let parseError = ''
  try { config = parseProfileConfig(value) } catch (error) { parseError = (error as Error).message }
  const endpoint = profileEndpoint(config, typeId)
  const issues = profileConfigIssues(config, typeId)
  const basic = ['tcp-banner', 'redis-emulator', 'ssh-high-interaction', 'honeytoken-http', 'conpot', 'dionaea'].includes(typeId)
  const update = (changes: Record<string, unknown>) => onChange(JSON.stringify(updateEndpoint(config, typeId, changes), null, 2))

  return <div className="space-y-4">
    {!parseError && basic && endpoint && <fieldset className="space-y-3">
      <legend className="text-sm font-semibold mb-3">Основные настройки</legend>
      {serviceProtocols(typeId).length > 0 && <label className="label" htmlFor={`${id}-protocol`}>{typeId === 'conpot' ? 'Промышленный протокол' : 'Протокол сервиса'}
        <select id={`${id}-protocol`} className="input mt-1" value={String(endpoint.protocol ?? serviceProtocols(typeId)[0]?.id)}
          onChange={event => {
            const protocol = serviceProtocols(typeId).find(item => item.id === event.target.value)
            if (protocol) update({ protocol: protocol.id, port: protocol.port })
          }}>
          {serviceProtocols(typeId).map(protocol => <option key={protocol.id} value={protocol.id}>{protocol.label} · {protocol.transport}</option>)}
        </select>
        <span className="text-xs text-muted">Порт подставляется по протоколу; его можно изменить ниже.</span>
      </label>}
      <label className="label" htmlFor={`${id}-port`}>Порт приманки
        <input id={`${id}-port`} className="input mt-1" type="number" min={1} max={65535} required
          value={typeof endpoint.port === 'number' ? endpoint.port || '' : ''}
          aria-invalid={!!issues.port} aria-describedby={issues.port ? `${id}-port-error` : undefined}
          onChange={event => update({ port: event.target.value === '' ? null : Number(event.target.value) })} />
      </label>
      {issues.port && <p id={`${id}-port-error`} role="alert" className="text-sm text-danger">{issues.port}</p>}
      {typeId === 'tcp-banner' && <label className="label" htmlFor={`${id}-banner`}>Приветствие при подключении
        <textarea id={`${id}-banner`} className="input mt-1" rows={2} value={String(endpoint.banner ?? '')}
          onChange={event => update({ banner: event.target.value })} />
      </label>}
      {typeId === 'ssh-high-interaction' && <label className="label" htmlFor={`${id}-username`}>Логин приманки
        <input id={`${id}-username`} className="input mt-1" value={String(endpoint.username ?? '')}
          onChange={event => update({ username: event.target.value })} />
      </label>}
      {['ssh-high-interaction', 'redis-emulator'].includes(typeId) && <label className="label" htmlFor={`${id}-password`}>Пароль приманки
        <input id={`${id}-password`} className="input mt-1" type="password" autoComplete="new-password"
          value={String(endpoint.password ?? '')} onChange={event => update({ password: event.target.value })} />
      </label>}
      <p className="text-xs text-muted">Остальные параметры заполнены. Дополнительные сервисы и ограничения доступны в расширенных настройках.</p>
    </fieldset>}
    <details open={parseError || !basic || !endpoint ? true : undefined} className="border-t border-line pt-3">
      <summary className="min-h-11 cursor-pointer text-sm font-medium">Расширенные настройки · JSON</summary>
      <label className="label mt-3" htmlFor={`${id}-json`}>Конфигурация выбранного типа
        <textarea id={`${id}-json`} className="input mt-1 font-mono text-xs h-56 resize-y" value={value}
          onChange={event => onChange(event.target.value)} spellCheck={false}
          aria-invalid={!!parseError} aria-describedby={parseError ? `${id}-json-error` : `${id}-json-hint`} />
      </label>
      <p id={`${id}-json-hint`} className="text-xs text-muted">Изменения здесь и в основных полях синхронизируются. Секреты после создания скрываются в ответах сервера.</p>
      {parseError && <p id={`${id}-json-error`} role="alert" className="text-sm text-danger">{parseError}</p>}
    </details>
  </div>
}
