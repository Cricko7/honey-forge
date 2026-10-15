import { Select } from '@/components/Select'
import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { Bug, AlertTriangle, Shuffle } from 'lucide-react'
import { Modal } from '@/components/Modal'
import { LoadError, Spinner } from '@/components/ui'
import { ApiError } from '@/api/client'
import type { CatalogEntry, DeploymentNode } from '@/types'
import { levelLabel } from '@/lib/ui-labels'
import { serviceProtocols } from '@/lib/service-protocols'
import { initialProfileConfig, updateEndpoint } from '@/lib/profile-editor'

function randomFreePort(used: Set<number> | Map<number, unknown>): number {
  for (let i = 0; i < 100; i++) {
    const p = 1025 + Math.floor(Math.random() * (65500 - 1025))
    if (!used.has(p)) return p
  }
  return 1025
}

export function defaultConfig(typeId: string, port: number, protocol?: string): Record<string, unknown> {
  const management = { heartbeat_interval_seconds: 10, telemetry_flush_interval_ms: 500 }
  switch (typeId) {
    case 'conpot':
    case 'dionaea':
      return updateEndpoint(initialProfileConfig(typeId), typeId, { port, protocol: protocol ?? serviceProtocols(typeId)[0].id })
    case 'redis-emulator':
      return { services: [{ name: 'redis', port, password: 'S3cretTrap!' }], management }
    case 'honeytoken-http':
      return {
        services: [{ name: 'web', port }],
        tokens: [{ id: 'aws-key', kind: 'key', path: '/admin/credentials.txt', value: 'AKIAIOSFODNN7EXAMPLE' }],
        management,
      }
    case 'ssh-high-interaction':
      return {
        services: [{ name: 'ssh', port, username: 'root', password: 'toor' }],
        session: { max_seconds: 300, max_connections: 4, max_recording_bytes: 1048576, max_upload_bytes: 1048576 },
        management,
      }
    default: // tcp-banner
      return {
        listeners: [{ name: 'svc', port, banner: 'SSH-2.0-OpenSSH_9.6\r\n', close_after_banner: false }],
        logging: { capture_payload: true, max_payload_bytes: 4096 },
        management,
      }
  }
}

export function AddTrapModal({
  open,
  onClose,
  catalog,
  catalogLoading,
  catalogError,
  onRetryCatalog,
  usedPorts,
  deploymentNodes,
  deploymentNodesError,
  deploymentNodeId,
  onDeploymentNodeChange,
  onCreate,
  creating,
}: {
  open: boolean
  onClose: () => void
  catalog: CatalogEntry[]
  catalogLoading: boolean
  catalogError: boolean
  onRetryCatalog: () => void
  deploymentNodes: DeploymentNode[]
  deploymentNodesError: boolean
  deploymentNodeId: string
  onDeploymentNodeChange: (id: string) => void
  usedPorts: Map<number, string>
  onCreate: (name: string, entry: CatalogEntry, port: number, protocol?: string) => Promise<void>
  creating: boolean
}) {
  const [name, setName] = useState('')
  const [typeKey, setTypeKey] = useState('')
  const [port, setPort] = useState<number>(0)
  const [error, setError] = useState<string | null>(null)
  const [protocol, setProtocol] = useState('modbus')

  const selected = useMemo(
    () => catalog.find((e) => `${e.type_id}/${e.type_version}` === typeKey),
    [catalog, typeKey],
  )

  useEffect(() => {
    if (open && catalog.length && !typeKey) {
      const first = catalog[0]
      setTypeKey(`${first.type_id}/${first.type_version}`)
    }
  }, [open, catalog, typeKey])

  useEffect(() => {
    if (open) setPort(randomFreePort(usedPorts))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  useEffect(() => {
    const first = serviceProtocols(selected?.type_id ?? '')[0]
    if (first) setProtocol(first.id)
  }, [selected?.type_id])

  const portValid = Number.isInteger(port) && port >= 1 && port <= 65535
  const conflictTrap = portValid ? usedPorts.get(port) : undefined
  const hasConflict = !!conflictTrap
  const nodeReady = !deploymentNodeId || deploymentNodes.some((node) => node.id === deploymentNodeId && node.ready)
  const isCustomHTTP = selected?.type_id === 'custom-http'
  const canSubmit = !catalogError && !catalogLoading && nodeReady && !!selected && !isCustomHTTP && portValid && !hasConflict && !creating

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    setError(null)
    if (!canSubmit) return
    if (!name.trim()) { setError('Укажите имя ловушки.'); return }
    try {
      await onCreate(name.trim(), selected, port, serviceProtocols(selected.type_id).length ? protocol : undefined)
      setName('')
      setTypeKey('')
    } catch (err) {
      if (err instanceof ApiError) setError(err.message || err.code)
      else setError('Не удалось создать ловушку')
    }
  }

  return (
    <Modal open={open} onClose={onClose} title="Новая ловушка" width="max-w-2xl">
      <form onSubmit={submit} className="space-y-5">
        <p className="text-sm text-muted">Выберите готовый тип или <Link to="/app/constructor" onClick={onClose} className="text-brand underline">соберите поведение из блоков</Link>.</p>
        {catalogLoading && <Spinner label="Загрузка типов ловушек…" />}
        {catalogError && <LoadError message="Не удалось загрузить типы ловушек." onRetry={onRetryCatalog} />}
        {!catalogLoading && !catalogError && !catalog.length && <p role="status" className="text-sm text-muted">Нет доступных типов. Обратитесь к администратору платформы.</p>}
        <div>
          <label className="label">Тип ловушки из каталога</label>
          <Select label="Тип ловушки из каталога" value={typeKey} onChange={setTypeKey}
            options={catalog.map(entry => ({ value: `${entry.type_id}/${entry.type_version}`, label: `${entry.title} · ${levelLabel(entry.interaction_level)}` }))} />
          {selected && <p className="mt-2 text-xs text-muted">{selected.description}</p>}
        </div>

        {isCustomHTTP && <div className="rounded-xl border border-brand/40 p-3 text-sm">
          Соберите поведение HTTP-ловушки из блоков и разверните её в конструкторе.
          <Link to="/app/constructor" onClick={onClose} className="block text-brand mt-2">Открыть конструктор ловушек →</Link>
        </div>}

        <div>
          <label className="label" htmlFor="deployment-node">Где развернуть ловушку</label>
          <Select id="deployment-node" label="Где развернуть ловушку" value={deploymentNodeId} onChange={onDeploymentNodeChange}
            options={[{ value: '', label: 'Сервер платформы' }, ...deploymentNodes.map((node) => ({ value: node.id, label: `${node.hostname} (${node.address})${!node.ready ? ' — недоступен' : ''}`, disabled: !node.ready }))]} />
          <p className="text-xs text-muted mt-2">
            Не видите нужный сервер? Подключите его на странице «Серверы» или обратитесь к администратору платформы.
          </p>
          <Link to="/app/servers" className="text-xs text-brand inline-block mt-2">Подключить сервер компании →</Link>
          {deploymentNodesError && <p role="alert" className="text-xs text-danger mt-2">Не удалось загрузить серверы. Повторная попытка выполняется автоматически.</p>}
          {!nodeReady && <p role="alert" className="text-xs text-danger mt-2">Выбранный сервер недоступен. Дождитесь подключения или выберите другой.</p>}
        </div>

        {selected && serviceProtocols(selected.type_id).length > 0 && <label className="label" htmlFor="trap-service-protocol">{selected.type_id === 'conpot' ? 'Промышленный протокол' : 'Протокол сервиса'}
          <select id="trap-service-protocol" className="input mt-1" value={protocol} onChange={event => {
            setProtocol(event.target.value)
            const option = serviceProtocols(selected.type_id).find(item => item.id === event.target.value)
            if (option) setPort(option.port)
          }}>
            {serviceProtocols(selected.type_id).map(item => <option key={item.id} value={item.id}>{item.label} · {item.transport}</option>)}
          </select>
        </label>}
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="label" htmlFor="trap-name">Имя ловушки</label>
            <input id="trap-name" className="input" required placeholder="dmz-ssh-03" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div>
            <div className="flex items-center justify-between mb-1.5">
              <label className="label mb-0" htmlFor="trap-port">Порт приманки</label>
              <button
                type="button"
                onClick={() => setPort(randomFreePort(usedPorts))}
                className="inline-flex items-center gap-1 text-[11px] font-medium text-muted hover:text-brand transition-colors"
                title="Подставить случайный свободный порт"
              >
                <Shuffle size={12} /> случайный
              </button>
            </div>
            <input
              id="trap-port"
              required
              aria-invalid={hasConflict || !portValid}
              aria-describedby="trap-port-hint"
              className={`input ${hasConflict || !portValid ? 'border-danger focus:ring-danger/40 focus:border-danger' : ''}`}
              type="number"
              min={1}
              max={65535}
              value={port || ''}
              onChange={(e) => setPort(Number(e.target.value))}
            />
          </div>
        </div>
        {!portValid ? (
          <div id="trap-port-hint" role="alert" className="flex items-start gap-3 rounded-xl border border-warn/30 bg-warn/10 p-3.5 animate-fadeIn">
            <div className="h-8 w-8 rounded-lg bg-warn/15 text-warn grid place-items-center shrink-0">
              <AlertTriangle size={16} />
            </div>
            <div className="text-sm">
              <div className="font-medium text-warn">Некорректный порт</div>
              <p className="text-xs text-warn/90 mt-0.5 leading-relaxed">
                Укажите номер порта от 1 до 65535.
              </p>
            </div>
          </div>
        ) : hasConflict ? (
          <div id="trap-port-hint" role="alert" className="flex items-start gap-3 rounded-xl border border-danger/30 bg-danger/10 p-3.5 animate-fadeIn">
            <div className="h-8 w-8 rounded-lg bg-danger/15 text-danger grid place-items-center shrink-0">
              <AlertTriangle size={16} />
            </div>
            <div className="min-w-0 text-sm">
              <div className="font-medium text-danger">Порт {port} уже занят</div>
              <p className="text-xs text-danger/80 mt-0.5 leading-relaxed">
                Ловушка «{conflictTrap}» уже слушает этот порт. На одном адресе два процесса не
                могут работать на одном порту — выберите другой.
              </p>
              <button
                type="button"
                onClick={() => setPort(randomFreePort(usedPorts))}
                className="mt-2 inline-flex items-center gap-1.5 text-xs font-medium text-danger hover:underline"
              >
                <Shuffle size={13} /> Подобрать свободный порт
              </button>
            </div>
          </div>
        ) : (
          <div id="trap-port-hint" className="flex items-start gap-2 text-sm text-muted bg-brand/5 border border-brand/15 rounded-xl p-3">
            <Bug size={14} className="text-brand mt-0.5 shrink-0" />
            Ловушка будет создана на выбранном сервере с портом {port}. После создания проверьте состояние запуска в карточке ловушки.
          </div>
        )}

        {error && (
          <div role="alert" className="flex items-start gap-3 rounded-xl border border-danger/30 bg-danger/10 p-3.5 animate-fadeIn">
            <div className="h-8 w-8 rounded-lg bg-danger/15 text-danger grid place-items-center shrink-0">
              <AlertTriangle size={16} />
            </div>
            <div className="text-sm">
              <div className="font-medium text-danger">Не удалось создать ловушку</div>
              <p className="text-xs text-danger/80 mt-0.5 leading-relaxed">{error}</p>
            </div>
          </div>
        )}

        <div className="flex justify-end gap-2">
          <button type="button" className="btn-ghost" onClick={onClose}>
            Отмена
          </button>
          <button type="submit" className="btn-primary" disabled={!canSubmit}>
            {creating ? 'Создание…' : 'Создать ловушку'}
          </button>
        </div>
      </form>
    </Modal>
  )
}
