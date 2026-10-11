import test from 'node:test'
import assert from 'node:assert/strict'
import { commandControls } from './command-controls.ts'

test('offline agent cannot receive a new command', () => {
  const state = commandControls({ connectivity: 'offline', active_command_id: null }, [], false)
  assert.equal(state.disabled, true)
  assert.equal(state.message, 'Агент отключён. Подключите агент для отправки команд.')
})

test('offline queued command is waiting rather than executing', () => {
  const state = commandControls({ connectivity: 'offline', active_command_id: 'a' }, [{ id: 'a', status: 'queued' }], false)
  assert.equal(state.disabled, true)
  assert.equal(state.message, 'Команда в очереди: ожидает подключения агента.')
})

test('online queued and running commands have distinct messages', () => {
  const trap = { connectivity: 'online', active_command_id: 'a' }
  assert.equal(commandControls(trap, [{ id: 'a', status: 'queued' }], false).message, 'Команда в очереди: ожидает выполнения агентом.')
  assert.equal(commandControls(trap, [{ id: 'a', status: 'running' }], false).message, 'Выполняется команда…')
})

test('completed command releases controls only once the trap has cleared its active command', () => {
  const commands = [{ id: 'a', status: 'expired' }]
  assert.equal(commandControls({ connectivity: 'online', active_command_id: 'a' }, commands, false).disabled, true)
  assert.deepEqual(commandControls({ connectivity: 'online', active_command_id: null }, commands, false), { disabled: false, message: null })
})

test('sending a request blocks repeated commands', () => {
  assert.equal(commandControls({ connectivity: 'online', active_command_id: null }, [], true).disabled, true)
})
