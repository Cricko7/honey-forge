import test from 'node:test'
import assert from 'node:assert/strict'
import { reconstructUpload } from './recording.ts'

const hash = '936a185caaa266bb9cbe981e9e05cb78cd732b0b3280eb944412bb6f8f8f07af'
const event = (event_type, data) => ({ event_type, data })
const recording = () => ({ events: [
  event('service.file_chunk', { file_id: 'file', offset: 5, payload_base64: 'd29ybGQ=' }),
  event('service.file_chunk', { file_id: 'file', offset: 0, payload_base64: 'aGVsbG8=' }),
  event('service.file_closed', { file_id: 'file', size: 10, sha256: hash, complete: true }),
] })

test('reconstructs out-of-order blocks and verifies SHA-256', async () => {
  const bytes = await reconstructUpload(recording(), 'file')
  assert.equal(new TextDecoder().decode(bytes), 'helloworld')
})

test('rejects a missing block even when a file_closed exists', async () => {
  const r = recording(); r.events.splice(0, 1)
  await assert.rejects(() => reconstructUpload(r, 'file'), /SHA-256/)
})

test('rejects incomplete, oversized and invalid-offset artifacts before download', async () => {
  for (const edit of [
    (r) => { r.events[2].data.complete = false },
    (r) => { r.events[2].data.size = 2 ** 40 },
    (r) => { r.events[0].data.offset = -1 },
  ]) {
    const r = recording(); edit(r)
    await assert.rejects(() => reconstructUpload(r, 'file'))
  }
})
