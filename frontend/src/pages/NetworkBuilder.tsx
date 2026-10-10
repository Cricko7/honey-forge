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
import { Plus, Save, Bug, Database, Server, Zap } from 'lucide-react'
import { TrapNode, type TrapNodeData } from './network/TrapNode'
import { useTraps, useProfiles, useCreateTrap, useLiveStream } from '@/api/hooks'
import { Modal } from '@/components/Modal'
import { useThemeColors } from '@/lib/theme'
import type { Profile } from '@/types'

const ATTACK = '#f43f5e'

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
  const navigate = useNavigate()
  const c = useThemeColors()
  const { data: trapsPage } = useTraps()
  const { data: profilesPage } = useProfiles()
  const createTrap = useCreateTrap()

  const traps = useMemo(() => trapsPage?.items ?? [], [trapsPage])
  const profiles = profilesPage?.items ?? []

  const [nodes, setNodes, onNodesChange] = useNodesState([])
  const [edges, setEdges, onEdgesChange] = useEdgesState([])
  const [attackPulse, setAttackPulse] = useState<Record<string, number>>({})
  const [addOpen, setAddOpen] = useState(false)
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
      draggable: true,
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
  }, [traps, attackPulse, c])

  const onConnect = useCallback(
    (c: Connection) => setEdges((eds) => addEdge({ ...c, markerEnd: { type: MarkerType.ArrowClosed } }, eds)),
    [setEdges],
  )

  function saveLayout() {
    const layout: Record<string, { x: number; y: number }> = {}
    nodes.forEach((n) => (layout[n.id] = n.position))
    localStorage.setItem(LAYOUT_KEY, JSON.stringify(layout))
    savedLayout.current = layout
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
            Карта ловушек организации · перетаскивайте узлы, стройте связи, кликните для деталей
          </p>
        </div>
        <div className="flex items-center gap-2">
          {attackCount > 0 && (
            <span className="chip bg-danger/10 text-danger animate-pulseGlow">
              <Zap size={12} /> {attackCount} под атакой
            </span>
          )}
          <button className="btn-ghost" onClick={saveLayout}>
            <Save size={16} /> Сохранить раскладку
          </button>
          <button className="btn-primary" onClick={() => setAddOpen(true)}>
            <Plus size={16} /> Добавить ловушку
          </button>
        </div>
      </div>

      <div className="flex-1 relative">
        <ReactFlow
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onConnect={onConnect}
          onNodeClick={onNodeClick}
          nodeTypes={nodeTypes}
          fitView
          proOptions={{ hideAttribution: true }}
          defaultEdgeOptions={{ style: { stroke: c.line } }}
        >
          <Background variant={BackgroundVariant.Dots} gap={22} size={1} color={c.line} />
          <Controls className="!bg-bg-panel !border-line !rounded-xl [&_button]:!bg-bg-elevated [&_button]:!border-line [&_button]:!text-fg [&_button]:!fill-current" />
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
        open={addOpen}
        onClose={() => setAddOpen(false)}
        profiles={profiles}
        creating={createTrap.isPending}
        onCreate={async (name, profileId) => {
          await createTrap.mutateAsync({ name, profile_id: profileId })
          setAddOpen(false)
        }}
      />
    </div>
  )
}

function AddTrapModal({
  open,
  onClose,
  profiles,
  onCreate,
  creating,
}: {
  open: boolean
  onClose: () => void
  profiles: Profile[]
  onCreate: (name: string, profileId: string) => void
  creating: boolean
}) {
  const [name, setName] = useState('')
  const [profileId, setProfileId] = useState('')

  useEffect(() => {
    if (open && profiles.length && !profileId) setProfileId(profiles[0].id)
  }, [open, profiles, profileId])

  return (
    <Modal open={open} onClose={onClose} title="Новая ловушка">
      <div className="space-y-4">
        <div>
          <label className="label">Имя ловушки</label>
          <input
            className="input"
            placeholder="dmz-ssh-03"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </div>
        <div>
          <label className="label">Профиль (тип + конфиг)</label>
          <select className="input" value={profileId} onChange={(e) => setProfileId(e.target.value)}>
            {profiles.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name} — {p.type_id}/{p.type_version} ({p.interaction_level})
              </option>
            ))}
          </select>
        </div>
        <div className="flex items-start gap-2 text-xs text-muted bg-bg-elevated/60 rounded-xl p-3">
          <Bug size={14} className="text-brand-soft mt-0.5 shrink-0" />
          Ловушка создаётся offline и в состоянии unknown. Реквизиты агента и запуск — на странице
          деталей ловушки.
        </div>
        <div className="flex justify-end gap-2">
          <button className="btn-ghost" onClick={onClose}>
            Отмена
          </button>
          <button
            className="btn-primary"
            disabled={!name || !profileId || creating}
            onClick={() => onCreate(name, profileId)}
          >
            {creating ? 'Создание…' : 'Создать'}
          </button>
        </div>
      </div>
    </Modal>
  )
}
