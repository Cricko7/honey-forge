export const qk = {
  trapLists: ['traps'] as const,
  eventLists: ['events'] as const,
  session: ['session'] as const,
  catalog: ['catalog'] as const,
  profiles: ['profiles'] as const,
  traps: (f?: object) => ['traps', f ?? {}] as const,
  trap: (id: string) => ['trap', id] as const,
  commands: (trapId: string) => ['commands', trapId] as const,
  events: (f?: object) => ['events', f ?? {}] as const,
  event: (id: string) => ['event', id] as const,
  trapLinks: ['trap-links'] as const,
}
