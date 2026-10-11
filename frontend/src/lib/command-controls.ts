import type { Command, Trap } from '../types'

export function commandControls(
  trap: Pick<Trap, 'connectivity' | 'active_command_id'>,
  commands: Pick<Command, 'id' | 'status'>[],
  sending: boolean,
): { disabled: boolean; message: string | null } {
  if (sending) return { disabled: true, message: 'Отправка команды…' }
  if (trap.active_command_id) {
    const command = commands.find((c) => c.id === trap.active_command_id)
    if (command?.status === 'queued') return {
      disabled: true,
      message: trap.connectivity === 'online'
        ? 'Команда в очереди: ожидает выполнения агентом.'
        : 'Команда в очереди: ожидает подключения агента.',
    }
    if (command?.status === 'running') return {
      disabled: true,
      message: trap.connectivity === 'online'
        ? 'Выполняется команда…'
        : 'Агент отключён. Ожидается результат команды.',
    }
    return { disabled: true, message: 'Обновление состояния команды…' }
  }
  if (trap.connectivity !== 'online') return {
    disabled: true,
    message: 'Агент отключён. Подключите агент для отправки команд.',
  }
  return { disabled: false, message: null }
}
