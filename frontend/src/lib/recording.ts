import type { SessionRecording } from '@/types'

const MAX_FILE_BYTES = 16 * 1024 * 1024

export function decodeRecordingBlock(value: unknown): Uint8Array {
  if (typeof value !== 'string') throw new Error('Некорректный блок записи')
  return Uint8Array.from(atob(value), (c) => c.charCodeAt(0))
}

export async function reconstructUpload(recording: SessionRecording, id: string): Promise<Uint8Array> {
  const closing = recording.events.find((e) => e.event_type === 'service.file_closed' && e.data.file_id === id)
  const size = closing?.data.size
  if (closing?.data.complete !== true || typeof size !== 'number' || !Number.isSafeInteger(size) || size < 0 || size > MAX_FILE_BYTES)
    throw new Error('Файл записан не полностью. Сохраните JSON для анализа частичной загрузки.')
  const content = new Uint8Array(size)
  for (const event of recording.events) {
    if (event.event_type !== 'service.file_chunk' || event.data.file_id !== id) continue
    const offset = event.data.offset
    const chunk = decodeRecordingBlock(event.data.payload_base64)
    if (typeof offset !== 'number' || !Number.isSafeInteger(offset) || offset < 0 || offset + chunk.length > size)
      throw new Error('Повреждённая запись файла')
    content.set(chunk, offset)
  }
  const digest = await crypto.subtle.digest('SHA-256', content)
  const hash = Array.from(new Uint8Array(digest), (v) => v.toString(16).padStart(2, '0')).join('')
  if (hash !== closing.data.sha256) throw new Error('SHA-256 не совпадает: в записи отсутствуют или повреждены блоки')
  return content
}
