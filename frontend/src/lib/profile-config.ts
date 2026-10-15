// Порты, которые слушает профиль: tcp-banner держит их в listeners[],
// остальные типы (redis/ssh/honeytoken) — в services[].
export function configPorts(config: Record<string, unknown>): number[] {
  const ports: number[] = []
  for (const key of ['listeners', 'services']) {
    const arr = config[key]
    if (!Array.isArray(arr)) continue
    for (const item of arr) {
      const p = (item as { port?: unknown } | null)?.port
      if (typeof p === 'number' && Number.isInteger(p) && p >= 1 && p <= 65535) ports.push(p)
    }
  }
  return ports
}

export function parseProfileConfig(text: string): Record<string, unknown> {
  let config: unknown
  try {
    config = JSON.parse(text)
  } catch {
    throw new Error('Конфигурация должна быть корректным JSON.')
  }
  if (config === null || typeof config !== 'object' || Array.isArray(config)) {
    throw new Error('Конфигурация должна быть JSON-объектом.')
  }
  return config as Record<string, unknown>
}
