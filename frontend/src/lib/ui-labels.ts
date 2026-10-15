import type { CommandAction, CommandStatus, RuntimeState } from '../types'

export const runtimeLabels: Record<RuntimeState, string> = {
  running: 'Работает', stopped: 'Остановлена', error: 'Ошибка', unknown: 'Неизвестно',
}

export const commandLabels: Record<CommandAction, string> = {
  start: 'Запуск', stop: 'Остановка', apply_config: 'Применение настроек',
}

export const commandStatusLabels: Record<CommandStatus, string> = {
  queued: 'В очереди', running: 'Выполняется', succeeded: 'Выполнено', failed: 'Ошибка', expired: 'Время ожидания истекло',
}

const eventLabels: Record<string, string> = {
  'tcp.connection_opened': 'TCP: новое подключение',
  'tcp.connection_closed': 'TCP: подключение закрыто',
  'tcp.payload_received': 'TCP: получены данные',
  'service.connection_opened': 'Новое подключение к сервису',
  'service.connection_closed': 'Подключение к сервису закрыто',
  'service.auth_attempt': 'Попытка входа',
  'ssh.auth_attempt': 'SSH: попытка входа',
  'service.action': 'Команда сервиса',
  'service.payload': 'Данные сервиса',
  'service.session_io': 'Ввод и вывод сессии',
  'service.file_opened': 'Открыт файл',
  'service.file_chunk': 'Переданы данные файла',
  'service.file_closed': 'Файл закрыт',
  'honeytoken.triggered': 'Приманка сработала',
  'http.custom': 'Событие конструктора',
  'process.exec': 'Запуск процесса',
}

export const eventLabel = (type: string) => eventLabels[type] ?? type

export const levelLabel = (level: string) => ({ low: 'Низкий', medium: 'Средний', high: 'Высокий' })[level] ?? level
