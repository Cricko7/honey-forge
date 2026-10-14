type Link = { source_id: string; target_id: string }

export function trapLinkLabel(link: Link, config?: Record<string, unknown>): string {
  const blocks = config?.blocks
  if (!Array.isArray(blocks)) return 'копия трафика'

  const sendsJSON = blocks.some((block) => block?.kind === 'send' && block.config?.target_id === link.target_id)

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
