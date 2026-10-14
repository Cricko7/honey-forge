import { Select } from '@/components/Select'
import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { Bug, Database, Server, KeyRound, TerminalSquare, AlertTriangle, Shuffle } from 'lucide-react'
import { Modal } from '@/components/Modal'
import { LevelBadge } from '@/components/ui'
import { ApiError } from '@/api/client'
import type { CatalogEntry, DeploymentNode } from '@/types'

// Случайный порт из «пользовательского» диапазона, не пересекающийся с занятыми.
function randomFreePort(used: Set<number> | Map<number, unknown>): number {
  for (let i = 0; i < 100; i++) {
    const p = 1025 + Math.floor(Math.random() * (65500 - 1025))
    if (!used.has(p)) return p
  }
  return 1025
}

// Иконка и дефолтный порт для каждого типа каталога.
const TYPE_ICON: Record<string, typeof Bug> = {
  'tcp-banner': Server,
  'redis-emulator': Database,
  'honeytoken-http': KeyRound,
  'ssh-high-interaction': TerminalSquare,
}
// Валидный дефолт-конфиг под config_schema типа — чтобы создавать ловушку
// любого типа прямо из конструктора без ручного JSON.
export function defaultConfig(typeId: string, port: number): Record<string, unknown> {
  const management = { heartbeat_interval_seconds: 10, telemetry_flush_interval_ms: 500 }
  switch (typeId) {
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
  deploymentNodes: DeploymentNode[]
  deploymentNodesError: boolean
  deploymentNodeId: string
  onDeploymentNodeChange: (id: string) => void
  usedPorts: Map<number, string>
  onCreate: (name: string, entry: CatalogEntry, port: number) => Promise<void>
  creating: boolean
}) {
  const [name, setName] = useState('')
  const [typeKey, setTypeKey] = useState('')
  const [port, setPort] = useState<number>(0)
  const [error, setError] = useState<string | null>(null)

  const selected = useMemo(
    () => catalog.find((e) => `${e.type_id}/${e.type_version}` === typeKey),
    [catalog, typeKey],
  )

  // Выбрать первый тип при открытии.
  useEffect(() => {
    if (open && catalog.length && !typeKey) {
      const first = catalog[0]
      setTypeKey(`${first.type_id}/${first.type_version}`)
    }
  }, [open, catalog, typeKey])

  // Каждый раз при открытии формы подставляем новый случайный свободный порт.
  useEffect(() => {
    if (open) setPort(randomFreePort(usedPorts))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const portValid = Number.isInteger(port) && port >= 1 && port <= 65535
  const conflictTrap = portValid ? usedPorts.get(port) : undefined
  const hasConflict = !!conflictTrap
  const nodeReady = !deploymentNodeId || deploymentNodes.some((node) => node.id === deploymentNodeId && node.ready)
  const isCustomHTTP = selected?.type_id === 'custom-http'
  const canSubmit = nodeReady && !!name.trim() && !!selected && !isCustomHTTP && portValid && !hasConflict && !creating

  async function submit() {
    setError(null)
    if (!nodeReady || !selected || isCustomHTTP || !name.trim() || !portValid || hasConflict) return
    try {
      await onCreate(name.trim(), selected, port)
      setName('')
      setTypeKey('')
    } catch (err) {
      if (err instanceof ApiError) setError(err.message || err.code)
      else setError('Не удалось создать ловушку')
    }
  }

  return (
    <Modal open={open} onClose={onClose} title="Новая ловушка" width="max-w-2xl">
      <div className="space-y-5">
        {/* Каталог типов прямо в конструкторе */}
        <div>
          <label className="label">Тип ловушки из каталога</label>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-2.5">
            {catalog.map((e) => {
              const key = `${e.type_id}/${e.type_version}`
              const Icon = TYPE_ICON[e.type_id] ?? Bug
              const active = key === typeKey
              return (
                <button
                  key={key}
                  type="button"
                  onClick={() => setTypeKey(key)}
                  className={`text-left rounded-xl border p-3 transition-colors ${
                    active
                      ? 'border-brand/60 bg-brand/10'
                      : 'border-line/60 bg-bg-elevated/50 hover:bg-bg-hover'
                  }`}
                >
                  <div className="flex items-center gap-2 mb-1.5">
                    <div className={`h-7 w-7 rounded-lg grid place-items-center ${active ? 'bg-brand/15 text-brand' : 'bg-bg-hover text-muted'}`}>
                      <Icon size={15} />
                    </div>
                    <span className="text-sm font-medium text-fg">{e.title}</span>
                    <span className="ml-auto">
                      <LevelBadge level={e.interaction_level} />
                    </span>
                  </div>
                  <p className="text-xs text-muted line-clamp-2">{e.description}</p>
                </button>
              )
            })}
          </div>
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
            Сервер компании появится после подключения к Docker Swarm и назначения вашей организации администратором платформы.
          </p>
          <Link to="/app/servers" className="text-xs text-brand inline-block mt-2">Подключить сервер компании →</Link>
          {deploymentNodesError && <p role="alert" className="text-xs text-danger mt-2">Не удалось загрузить серверы. Повторная попытка выполняется автоматически.</p>}
          {!nodeReady && <p role="alert" className="text-xs text-danger mt-2">Выбранный сервер недоступен. Дождитесь подключения или выберите другой.</p>}
        </div>

        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="label">Имя ловушки</label>
            <input className="input" placeholder="dmz-ssh-03" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div>
            <div className="flex items-center justify-between mb-1.5">
              <label className="label mb-0">Порт приманки</label>
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
              className={`input ${hasConflict || !portValid ? 'border-danger focus:ring-danger/40 focus:border-danger' : ''}`}
              type="number"
              min={1}
              max={65535}
              value={port || ''}
              onChange={(e) => setPort(Number(e.target.value))}
            />
          </div>
        </div>

        {/* Премиальное сообщение о конфликте/некорректном порте */}
        {!portValid ? (
          <div role="alert" className="flex items-start gap-3 rounded-xl border border-warn/30 bg-warn/10 p-3.5 animate-fadeIn">
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
          <div role="alert" className="flex items-start gap-3 rounded-xl border border-danger/30 bg-danger/10 p-3.5 animate-fadeIn">
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
          <div className="flex items-start gap-2 text-xs text-muted bg-brand/5 border border-brand/15 rounded-xl p-3">
            <Bug size={14} className="text-brand mt-0.5 shrink-0" />
            Панель создаст профиль, поднимет контейнер приманки на выбранном сервере и
            запустит listener на порту {port}. Тонкую настройку конфига можно сделать на странице
            «Профили».
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
          <button className="btn-ghost" onClick={onClose}>
            Отмена
          </button>
          <button className="btn-primary" disabled={!canSubmit} onClick={submit}>
            {creating ? 'Создание…' : 'Создать ловушку'}
          </button>
        </div>
      </div>
    </Modal>
  )
}
