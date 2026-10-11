export function parseProfileConfig(text: string): Record<string, unknown> {
  let config: unknown
  try {
    config = JSON.parse(text)
  } catch {
    throw new Error('config не является корректным JSON')
  }
  if (config === null || typeof config !== 'object' || Array.isArray(config)) {
    throw new Error('config должен быть JSON-объектом')
  }
  return config as Record<string, unknown>
}
