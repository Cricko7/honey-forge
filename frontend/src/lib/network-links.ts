type Link = { source_id: string; target_id: string }

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
