import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import ReactFlow, {
  Background,
  BackgroundVariant,
  Controls,
  MiniMap,
  useEdgesState,
  useNodesState,
  type Connection,
  type Edge,
  type EdgeChange,
  type Node,
  type NodeTypes,
  MarkerType,
} from 'reactflow'
import 'reactflow/dist/style.css'
import { Plus, Server, Database, Zap, RotateCcw, Trash2 } from 'lucide-react'
import { TrapNode, type TrapNodeData } from './network/TrapNode'
import { AddTrapModal, defaultConfig } from './network/AddTrapModal'
import { useDeploymentNodes, useCanManage, useTraps, useCatalog, useCreateProfile, useCreateTrap, useLiveStream, useProfiles, useTrapLinks, useCreateTrapLink, useDeleteTrapLink } from '@/api/hooks'
import { configPorts } from '@/lib/profile-config'
import { ApiError } from '@/api/client'
import { canConnectTraps, trapLinkKey, trapLinkLabel } from '@/lib/network-links'
import { useThemeColors } from '@/lib/theme'

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
  const canManage = useCanManage()
  const navigate = useNavigate()
  const c = useThemeColors()
  const { data: trapsPage } = useTraps()
  const { data: deploymentNodes, error: deploymentNodesError } = useDeploymentNodes()
  const [deploymentNodeId, setDeploymentNodeId] = useState('')
  const { data: catalogPage } = useCatalog()
  const { data: profilesPage } = useProfiles()
  const createProfile = useCreateProfile()
  const createTrap = useCreateTrap()
  const { data: linksPage, error: linksError } = useTrapLinks()
  const createLink = useCreateTrapLink()
  const deleteLink = useDeleteTrapLink()
  const links = useMemo(() => linksPage?.items ?? [], [linksPage])
  const [linkError, setLinkError] = useState<string | null>(null)

  const traps = useMemo(() => trapsPage?.items ?? [], [trapsPage])

  const usedPorts = useMemo(() => {
    const profiles = profilesPage?.items ?? []
    const byId = new Map(profiles.map((p) => [p.id, p]))
    const map = new Map<number, string>()
    for (const trap of traps) {
      if (deploymentNodeId && trap.deployment_node_id && trap.deployment_node_id !== deploymentNodeId) continue
      const profile = byId.get(trap.profile_id)
      if (!profile) continue
      for (const port of configPorts(profile.config)) {
        if (!map.has(port)) map.set(port, trap.name)
      }
    }
    return map
  }, [traps, profilesPage, deploymentNodeId])
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
      deletable: false,
    }

    const trapNodes: Node<TrapNodeData>[] = traps.map((t, i) => ({
      deletable: false,
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
      const autoEdges: Edge[] = traps.map((t) => ({
        id: `gateway-${t.id}`,
        source: 'gateway',
        target: t.id,
        deletable: false,
        animated: !!attackPulse[t.id],
        style: {
          stroke: attackPulse[t.id] ? ATTACK : c.line,
          strokeWidth: attackPulse[t.id] ? 2.5 : 1.5,
        },
        markerEnd: { type: MarkerType.ArrowClosed, color: attackPulse[t.id] ? ATTACK : c.line },
      }))
      const userEdges: Edge[] = links
        .filter((link) => traps.some((t) => t.id === link.source_id) && traps.some((t) => t.id === link.target_id))
        .map((link) => ({
          id: trapLinkKey(link), source: link.source_id, target: link.target_id,
          type: 'smoothstep',
          selected: prev.find((edge) => edge.id === trapLinkKey(link))?.selected,
          label: trapLinkLabel(link, profilesPage?.items.find((p) => p.id === traps.find((t) => t.id === link.source_id)?.profile_id)?.config),
          style: { stroke: c.brand, strokeWidth: 2 },
          labelStyle: { fill: c.brand, fontSize: 11 },
          labelBgStyle: { fill: c.panel },
          markerEnd: { type: MarkerType.ArrowClosed, color: c.brand },
        }))
      return [...autoEdges, ...userEdges]
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [traps, links, profilesPage, attackPulse, c, canManage, layoutRevision])

  const onConnect = useCallback((connection: Connection) => {
    if (!canManage || createLink.isPending || !canConnectTraps(connection.source, connection.target, traps, links)) return
    setLinkError(null)
    createLink.mutate({ source_id: connection.source!, target_id: connection.target! }, {
      onError: (err) => setLinkError(err instanceof ApiError ? err.message : 'Не удалось сохранить связь'),
    })
  }, [canManage, createLink, traps, links])

  function removeLink(edge: Edge) {
    if (!canManage || deleteLink.isPending || edge.source === 'gateway') return
    setLinkError(null)
    deleteLink.mutate({ source_id: edge.source, target_id: edge.target }, {
      onError: (err) => setLinkError(err instanceof ApiError ? err.message : 'Не удалось удалить связь'),
    })
  }

  function changeEdges(changes: EdgeChange[]) {
    onEdgesChange(changes.filter((change) => change.type !== 'remove'))
    for (const change of changes) {
      if (change.type === 'remove') {
        const edge = edges.find((edge) => edge.id === change.id)
        if (edge) removeLink(edge)
      }
    }
  }

  const selectedLink = edges.find((edge) => edge.selected && edge.source !== 'gateway')

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
            {canManage ? 'A → B разрешает передачу данных · HTTP-сценарий отправки задаётся в конструкторе ловушек' : 'Карта ловушек организации · только просмотр, кликните для деталей'}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {canManage && selectedLink && <button className="btn-ghost text-danger" disabled={deleteLink.isPending} onClick={() => removeLink(selectedLink)}>
            <Trash2 size={16} /> Удалить связь
          </button>}
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
        {(linkError || linksError) && <div role="alert" className="absolute top-4 left-4 z-20 card px-4 py-3 text-danger text-sm">
          {linkError ?? 'Не удалось загрузить связи ловушек'}
        </div>}
        <ReactFlow
          key={nodes.length > 1 ? 'traps' : 'gateway'}
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={canManage ? changeEdges : undefined}
          onConnect={canManage ? onConnect : undefined}
          isValidConnection={(connection) => canManage && !!linksPage && !createLink.isPending && canConnectTraps(connection.source, connection.target, traps, links)}
          nodesDraggable={canManage}
          nodesConnectable={canManage}
          connectOnClick={false}
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
        <div className="absolute bottom-4 left-4 card px-4 py-3 text-xs space-y-1.5 z-10">
          <div className="font-semibold text-fg mb-1">Легенда</div>
          <div className="flex items-center gap-2 text-muted"><span className="text-brand-soft">→</span> Передача JSON · настроенный HTTP-сценарий</div>
          <div className="flex items-center gap-2 text-muted"><span className="text-brand-soft">→</span> Копия трафика · входящие байты</div>
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
        deploymentNodes={deploymentNodes?.items ?? []}
        deploymentNodesError={!!deploymentNodesError}
        deploymentNodeId={deploymentNodeId}
        onDeploymentNodeChange={setDeploymentNodeId}
        creating={createProfile.isPending || createTrap.isPending}
        onCreate={async (name, entry, port) => {
          const config = defaultConfig(entry.type_id, port)
          const profile = await createProfile.mutateAsync({
            name,
            type_id: entry.type_id,
            type_version: entry.type_version,
            config,
          })
          await createTrap.mutateAsync({ name, profile_id: profile.id, ...(deploymentNodeId ? { deployment_node_id: deploymentNodeId } : {}) })
          setAddOpen(false)
        }}
      />
    </div>
  )
}
