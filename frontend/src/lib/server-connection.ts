export function workerJoinCommand(manager: string, token: string): string {
  const address = manager.trim()
  const secret = token.trim()
  if (!/^[a-zA-Z0-9.\[\]:-]+:\d{1,5}$/.test(address) || !/^SWMTKN-[a-zA-Z0-9-]+$/.test(secret)) return ''
  const port = Number(address.slice(address.lastIndexOf(':') + 1))
  if (port < 1 || port > 65535) return ''

  return `docker swarm join --token ${secret} ${address}`
}

export function assignServerCommand(node: string, organization: string): string {
  if (!/^[a-z0-9]{25}$/.test(node.trim()) || !/^[a-f0-9]{8}(-[a-f0-9]{4}){3}-[a-f0-9]{12}$/i.test(organization)) return ''

  return `docker node update --label-add honeyforge.organization=${organization} ${node.trim()}`
}
