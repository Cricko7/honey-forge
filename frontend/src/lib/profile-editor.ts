export function initialProfileConfig(typeId: string): Record<string, unknown> {
  const management = { heartbeat_interval_seconds: 5, telemetry_flush_interval_ms: 500 }
  switch (typeId) {
    case 'dionaea':
      return { services: [{ name: 'files', port: 445, protocol: 'smb' }], session: { max_connections: 16, max_seconds: 300 }, management }
    case 'conpot':
      return { services: [{ name: 'plc', port: 502, protocol: 'modbus' }], session: { max_connections: 16, max_seconds: 300 }, management }
    case 'custom-http':
      return { services: [], blocks: [], limits: { max_steps: 512, timeout_ms: 10000, max_body_bytes: 65536 }, management }
    case 'ssh-high-interaction':
      return { services: [{ name: 'ssh', port: 2222, username: 'root', password: 'DemoTrapOnly!' }],
        session: { max_seconds: 300, max_connections: 4, max_recording_bytes: 1048576, max_upload_bytes: 1048576 }, management }
    case 'honeytoken-http': {
      const key = Array.from(crypto.getRandomValues(new Uint8Array(24)), byte => byte.toString(16).padStart(2, '0')).join('')
      return { services: [{ name: 'web', port: 8080 }], tokens: [
        { id: 'backup-key', kind: 'key', path: '/v1/backups', value: key },
        { id: 'backup-file', kind: 'file', path: '/backup.txt', value: 'Demo backup: no real data.' },
        { id: 'report-url', kind: 'url', path: '/reports/latest', value: 'Report ready.' },
      ], management }
    }
    case 'redis-emulator':
      return { services: [{ name: 'redis', port: 6379, password: 'S3cretTrap!' }], management }
    default:
      return { listeners: [{ name: 'ssh', port: 22, banner: 'SSH-2.0-OpenSSH_8.9', close_after_banner: false }],
        logging: { capture_payload: true, max_payload_bytes: 4096 }, management }
  }
}

export function profileEndpoint(config: Record<string, unknown>, typeId: string): Record<string, unknown> | null {
  const items = config[typeId === 'tcp-banner' ? 'listeners' : 'services']
  const first: unknown = Array.isArray(items) ? items[0] : undefined
  return first !== null && typeof first === 'object' && !Array.isArray(first) ? first as Record<string, unknown> : null
}

export function updateEndpoint(config: Record<string, unknown>, typeId: string, changes: Record<string, unknown>): Record<string, unknown> {
  const key = typeId === 'tcp-banner' ? 'listeners' : 'services'
  const items = config[key]
  if (!Array.isArray(items) || !profileEndpoint(config, typeId)) return config
  return { ...config, [key]: [{ ...items[0], ...changes }, ...items.slice(1)] }
}

export function profileConfigIssues(config: Record<string, unknown>, typeId: string): Record<string, string> {
  if (!['tcp-banner', 'ssh-high-interaction', 'redis-emulator', 'honeytoken-http', 'conpot', 'dionaea'].includes(typeId)) return {}
  const port = profileEndpoint(config, typeId)?.port
  return typeof port === 'number' && Number.isInteger(port) && port >= 1 && port <= 65535
    ? {} : { port: 'Укажите целый номер порта от 1 до 65535.' }
}
