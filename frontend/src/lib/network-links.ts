type Link = { source_id: string; target_id: string }

export function trapLinkHandles(source: { x: number; y: number }, target: { x: number; y: number }) {
  const dx = target.x - source.x
  const dy = target.y - source.y
  if (Math.abs(dx) > Math.abs(dy)) {
    return dx > 0 ? { sourceHandle: 'right', targetHandle: 'left' } : { sourceHandle: 'left', targetHandle: 'right' }
  }
  return dy >= 0 ? { sourceHandle: 'bottom', targetHandle: 'top' } : { sourceHandle: 'top', targetHandle: 'bottom' }
}

export function trapLinkLabel(link: Link, config?: Record<string, unknown>, targetType?: string): string {
  const blocks = config?.blocks
  if (!Array.isArray(blocks)) return 'копия трафика'

  const sendsJSON = blocks.some((block) => block?.kind === 'send' && (
    block.config?.target_id === link.target_id ||
    block.config?.target_id === '00000000-0000-0000-0000-000000000000' && targetType === 'custom-http'
  ))

  return sendsJSON ? 'передача JSON' : 'копия трафика'
}

export function trapLinkKey(link: Link): string {
  return `link:${link.source_id}:${link.target_id}`
}

export function canConnectTraps(
  source: string | null,
  target: string | null,
  traps: { id: string }[],
  links: Link[],
): boolean {
  return !!source && !!target && source !== target
    && traps.some((trap) => trap.id === source)
    && traps.some((trap) => trap.id === target)
    && !links.some((link) => link.source_id === source && link.target_id === target)
}
