import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import ReactFlow, {
  Background,
  BackgroundVariant,
  Controls,
  MiniMap,
  addEdge,
  useEdgesState,
  useNodesState,
  type Connection,
  type Edge,
  type Node,
  type NodeTypes,
  MarkerType,
} from 'reactflow'
import 'reactflow/dist/style.css'
import { Plus, Bug, Database, Server, Zap, KeyRound, TerminalSquare, RotateCcw, AlertTriangle, Shuffle } from 'lucide-react'
import { TrapNode, type TrapNodeData } from './network/TrapNode'
import { useCanManage, useTraps, useCatalog, useCreateProfile, useCreateTrap, useLiveStream, useProfiles } from '@/api/hooks'
import { Modal } from '@/components/Modal'
import { LevelBadge } from '@/components/ui'
import { ApiError } from '@/api/client'
import { configPorts } from '@/lib/profile-config'
import { useThemeColors } from '@/lib/theme'
import type { CatalogEntry } from '@/types'

// Случайный порт из «пользовательского» диапазона, не пересекающийся с занятыми.
function randomFreePort(used: Set<number> | Map<number, unknown>): number {
  for (let i = 0; i < 100; i++) {
    const p = 1025 + Math.floor(Math.random() * (65500 - 1025))
    if (!used.has(p)) return p
  }
  return 1025
}

const ATTACK = '#f43f5e'

// Иконка и дефолтный порт для каждого типа каталога.
const TYPE_ICON: Record<string, typeof Bug> = {
  'tcp-banner': Server,
  'redis-emulator': Database,
  'honeytoken-http': KeyRound,
  'ssh-high-interaction': TerminalSquare,
}
// Валидный дефолт-конфиг под config_schema типа — чтобы создавать ловушку
// любого типа прямо из конструктора без ручного JSON.
function defaultConfig(typeId: string, port: number): Record<string, unknown> {
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

const nodeTypes: NodeTypes = { trap: TrapNode }
const LAYOUT_KEY = 'honeyforge.network.layout.v1'

function loadLayout(): Record<string, { x: number; y: number }> {
  try {
    return JSON.parse(localStorage.getItem(LAYOUT_KEY) ?? '{}')
  } catch {
    return {}
  }
}

export function NetworkBuilderPage() {
  const canManage = useCanManage()
  const navigate = useNavigate()
  const c = useThemeColors()
  const { data: trapsPage } = useTraps()
  const { data: catalogPage } = useCatalog()
  const { data: profilesPage } = useProfiles()
  const createProfile = useCreateProfile()
  const createTrap = useCreateTrap()

  const traps = useMemo(() => trapsPage?.items ?? [], [trapsPage])

  // Порты, уже занятые ловушками (порт → имя ловушки): два процесса не могут
  // слушать один порт на одном адресе, поэтому дубли запрещаем ещё в форме.
  const usedPorts = useMemo(() => {
    const profiles = profilesPage?.items ?? []
    const byId = new Map(profiles.map((p) => [p.id, p]))
    const map = new Map<number, string>()
    for (const trap of traps) {
      const profile = byId.get(trap.profile_id)
      if (!profile) continue
      for (const port of configPorts(profile.config)) {
        if (!map.has(port)) map.set(port, trap.name)
      }
    }
    return map
  }, [traps, profilesPage])
  const catalog = useMemo(
    () => (catalogPage?.items ?? []).filter((e) => e.available_for_new_profiles),
    [catalogPage],
  )

  const [nodes, setNodes, onNodesChange] = useNodesState([])
  const [edges, setEdges, onEdgesChange] = useEdgesState([])
  const [attackPulse, setAttackPulse] = useState<Record<string, number>>({})
  const [addOpen, setAddOpen] = useState(false)
  const [layoutRevision, setLayoutRevision] = useState(0)
  const savedLayout = useRef(loadLayout())

  // Live: подсветка узла при событии атаки
  useLiveStream((e) => {
    setAttackPulse((p) => ({ ...p, [e.trap_id]: Date.now() }))
    setTimeout(() => {
      setAttackPulse((p) => {
        const next = { ...p }
        if (next[e.trap_id] && Date.now() - next[e.trap_id] >= 2500) delete next[e.trap_id]
        return next
      })
    }, 2600)
  })

  // Построение графа из ловушек + gateway
  useEffect(() => {
    const layout = savedLayout.current
    const gateway: Node<TrapNodeData> = {
      id: 'gateway',
      type: 'trap',
      position: layout['gateway'] ?? { x: 420, y: 20 },
      data: {
        label: 'Интернет',
        typeId: 'gateway',
        level: '',
        connectivity: 'online',
        runtime: 'running',
        kind: 'gateway',
      },
      draggable: canManage,
    }

    const trapNodes: Node<TrapNodeData>[] = traps.map((t, i) => ({
      id: t.id,
      type: 'trap',
      position: layout[t.id] ?? { x: 120 + (i % 4) * 230, y: 200 + Math.floor(i / 4) * 180 },
      data: {
        label: t.name,
        typeId: t.type_id,
        level: t.interaction_level,
        connectivity: t.connectivity,
        runtime: t.runtime_state,
        kind: 'trap',
        underAttack: !!attackPulse[t.id],
      },
    }))

    setNodes([gateway, ...trapNodes])

    setEdges((prev) => {
      // авто-связи gateway → ловушка (подсвечиваются при атаке)
      const autoEdges: Edge[] = traps.map((t) => ({
        id: `gateway-${t.id}`,
        source: 'gateway',
        target: t.id,
        animated: !!attackPulse[t.id],
        style: {
          stroke: attackPulse[t.id] ? ATTACK : c.line,
          strokeWidth: attackPulse[t.id] ? 2.5 : 1.5,
        },
        markerEnd: { type: MarkerType.ArrowClosed, color: attackPulse[t.id] ? ATTACK : c.line },
      }))
      // пользовательские связи (нарисованные вручную) сохраняем
      const userEdges = prev.filter((e) => !e.id.startsWith('gateway-'))
      return [...autoEdges, ...userEdges]
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [traps, attackPulse, c, canManage, layoutRevision])

  const onConnect = useCallback(
    (c: Connection) => setEdges((eds) => addEdge({ ...c, markerEnd: { type: MarkerType.ArrowClosed } }, eds)),
    [setEdges],
  )

  // Авто-сохранение раскладки: позиция каждого узла пишется в localStorage
  // при завершении перетаскивания — вручную ничего сохранять не нужно.
  function saveLayout() {
    const layout: Record<string, { x: number; y: number }> = {}
    nodes.forEach((n) => (layout[n.id] = n.position))
    localStorage.setItem(LAYOUT_KEY, JSON.stringify(layout))
    savedLayout.current = layout
  }

  function resetLayout() {
    localStorage.removeItem(LAYOUT_KEY)
    savedLayout.current = {}
    setLayoutRevision((revision) => revision + 1)
  }

  function onNodeClick(_: unknown, node: Node) {
    if (node.id !== 'gateway') navigate(`/app/traps/${node.id}`)
  }

  const attackCount = Object.keys(attackPulse).length

  return (
    <div className="h-full flex flex-col">
      <div className="flex items-center justify-between px-6 py-4 border-b border-line/60">
        <div>
          <h1 className="text-xl font-semibold">Конструктор сети</h1>
          <p className="text-sm text-muted">
            {canManage ? 'Карта ловушек организации · перетаскивайте узлы, стройте связи, кликните для деталей' : 'Карта ловушек организации · только просмотр, кликните для деталей'}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {attackCount > 0 && (
            <span className="chip bg-danger/10 text-danger animate-pulseGlow">
              <Zap size={12} /> {attackCount} под атакой
            </span>
          )}
          <button className="btn-ghost" onClick={resetLayout}>
            <RotateCcw size={16} /> Сбросить расположение
          </button>
          {canManage && <button className="btn-primary" onClick={() => setAddOpen(true)}>
            <Plus size={16} /> Добавить ловушку
          </button>}
        </div>
      </div>

      <div className="flex-1 relative">
        <ReactFlow
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={canManage ? onEdgesChange : undefined}
          onConnect={canManage ? onConnect : undefined}
          nodesDraggable={canManage}
          nodesConnectable={canManage}
          edgesFocusable={canManage}
          deleteKeyCode={canManage ? ['Backspace', 'Delete'] : null}
          onNodeClick={onNodeClick}
          onNodeDragStop={canManage ? () => saveLayout() : undefined}
          nodeTypes={nodeTypes}
          fitView
          proOptions={{ hideAttribution: true }}
          defaultEdgeOptions={{ style: { stroke: c.line } }}
        >
          <Background variant={BackgroundVariant.Dots} gap={22} size={1} color={c.line} />
          <Controls showInteractive={canManage} className="!bg-bg-panel !border-line !rounded-xl [&_button]:!bg-bg-elevated [&_button]:!border-line [&_button]:!text-fg [&_button]:!fill-current" />
          <MiniMap
            className="!bg-bg-panel !border !border-line !rounded-xl"
            maskColor={c.mask}
            nodeColor={(n) => ((n.data as TrapNodeData)?.underAttack ? ATTACK : c.brand)}
          />
        </ReactFlow>

        {/* Легенда */}
        <div className="absolute bottom-4 left-4 card px-4 py-3 text-xs space-y-1.5 z-10">
          <div className="font-semibold text-fg mb-1">Легенда</div>
          <div className="flex items-center gap-2 text-muted">
            <Server size={12} className="text-brand-soft" /> tcp-banner (Low)
          </div>
          <div className="flex items-center gap-2 text-muted">
            <Database size={12} className="text-brand-soft" /> redis-emulator (Medium)
          </div>
          <div className="flex items-center gap-2 text-muted">
            <span className="h-2 w-2 rounded-full bg-danger" /> под атакой
          </div>
        </div>
      </div>

      <AddTrapModal
        open={canManage && addOpen}
        onClose={() => setAddOpen(false)}
        catalog={catalog}
        usedPorts={usedPorts}
        creating={createProfile.isPending || createTrap.isPending}
        onCreate={async (name, entry, port) => {
          const config = defaultConfig(entry.type_id, port)
          const profile = await createProfile.mutateAsync({
            name,
            type_id: entry.type_id,
            type_version: entry.type_version,
            config,
          })
          await createTrap.mutateAsync({ name, profile_id: profile.id })
          setAddOpen(false)
        }}
      />
    </div>
  )
}

function AddTrapModal({
  open,
  onClose,
  catalog,
  usedPorts,
  onCreate,
  creating,
}: {
  open: boolean
  onClose: () => void
  catalog: CatalogEntry[]
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
  const canSubmit = !!name.trim() && !!selected && portValid && !hasConflict && !creating

  async function submit() {
    setError(null)
    if (!selected || !name.trim() || !portValid || hasConflict) return
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
            Панель сама создаст профиль по дефолт-конфигу типа, поднимет контейнер приманки и
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
