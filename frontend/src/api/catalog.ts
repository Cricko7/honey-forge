import type { CatalogEntry } from '@/types'

// Каталог типов ловушек — взят из api/03-catalog.md (immutable-версии).
// Два поддерживаемых типа: tcp-banner/1 (Low) и redis-emulator/1 (Medium).

export const CATALOG: CatalogEntry[] = [
  {
    type_id: 'tcp-banner',
    type_version: 1,
    title: 'TCP Banner',
    description:
      'Низкоинтерактивная ловушка: открывает TCP-порты, отдаёт баннер и логирует подключения и payload атакующего.',
    interaction_level: 'low',
    available_for_new_profiles: true,
    config_schema: {
      type: 'object',
      required: ['listeners', 'logging', 'management'],
      properties: {
        listeners: { type: 'array' },
        logging: { type: 'object' },
        management: { type: 'object' },
      },
    },
    event_schemas: [
      { event_type: 'tcp.connection_opened', title: 'Подключение открыто', data_schema: {} },
      { event_type: 'tcp.payload_received', title: 'Получен payload', data_schema: {} },
      { event_type: 'tcp.connection_closed', title: 'Подключение закрыто', data_schema: {} },
    ],
    actions: [
      { action: 'start', title: 'Запустить', params_schema: {}, result_schema: {} },
      { action: 'stop', title: 'Остановить', params_schema: {}, result_schema: {} },
      { action: 'apply_config', title: 'Применить конфиг', params_schema: {}, result_schema: {} },
    ],
    ui: {
      field_order: ['/listeners', '/logging', '/management'],
      widgets: {},
    },
  },
  {
    type_id: 'redis-emulator',
    type_version: 1,
    title: 'Redis Emulator',
    description:
      'Среднеинтерактивная ловушка: эмулирует Redis, ловит попытки аутентификации и команды атакующего (AUTH, CONFIG, KEYS…).',
    interaction_level: 'medium',
    available_for_new_profiles: true,
    config_schema: {
      type: 'object',
      required: ['services', 'management'],
      properties: {
        services: { type: 'array' },
        management: { type: 'object' },
      },
    },
    event_schemas: [
      { event_type: 'service.connection_opened', title: 'Подключение открыто', data_schema: {} },
      { event_type: 'service.auth_attempt', title: 'Попытка входа', data_schema: {} },
      { event_type: 'service.action', title: 'Команда сервиса', data_schema: {} },
      { event_type: 'service.connection_closed', title: 'Подключение закрыто', data_schema: {} },
    ],
    actions: [
      { action: 'start', title: 'Запустить', params_schema: {}, result_schema: {} },
      { action: 'stop', title: 'Остановить', params_schema: {}, result_schema: {} },
      { action: 'apply_config', title: 'Применить конфиг', params_schema: {}, result_schema: {} },
    ],
    ui: {
      field_order: ['/services', '/management'],
      widgets: { '/services/0/password': 'password' },
    },
  },
]

export function catalogKey(typeId: string, version: number): string {
  return `${typeId}/${version}`
}

export function findCatalogEntry(typeId: string, version: number): CatalogEntry | undefined {
  return CATALOG.find((c) => c.type_id === typeId && c.type_version === version)
}
