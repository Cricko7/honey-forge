import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/api/client'
import { Spinner } from './ui'
import type { SessionRecording } from '@/types'
import { decodeRecordingBlock, reconstructUpload } from '@/lib/recording'

function save(blob: Blob, name: string) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = name
  link.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

async function downloadFile(recording: SessionRecording, id: string) {
  const content = await reconstructUpload(recording, id)
  // Use a generated artifact ID, never an attacker-controlled filename or MIME type.
  save(new Blob([content], { type: 'application/octet-stream' }), `${id}.bin`)
}

export function RecordingView({ trapId, sessionId }: { trapId: string; sessionId: string }) {
  const [open, setOpen] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ['recording', trapId, sessionId], queryFn: () => api.getRecording(trapId, sessionId), enabled: open,
  })
  const files = data?.events.filter((e) => e.event_type === 'service.file_opened') ?? []
  const terminal = data?.events.filter((e) => e.event_type === 'service.session_io') ?? []
  return (
    <div className="space-y-3">
      <button className="btn-ghost" onClick={() => setOpen(!open)}>{open ? 'Скрыть запись' : 'Запись SSH-сессии'}</button>
      {open && isLoading && <Spinner />}
      {open && isError && <p className="text-danger text-xs">Не удалось загрузить запись</p>}
      {open && data && <>
        <p className="text-xs text-muted">{data.complete ? 'Сессия завершена, последовательность событий полная.' : 'Сессия ещё активна или запись содержит пропуски.'}</p>
        <div className="flex gap-2">
          <button className="btn-ghost" onClick={() => refetch()}>Обновить</button>
          <button className="btn-ghost" onClick={() => save(new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' }), `${sessionId}.json`)}>Сохранить JSON</button>
        </div>
        <pre className="text-xs bg-bg-elevated rounded-xl p-3 max-h-72 overflow-auto whitespace-pre-wrap">
          {terminal.map((e) => {
            let text = '(повреждённый блок)'
            try { text = new TextDecoder().decode(decodeRecordingBlock(e.data.payload_base64)) } catch { /* Preserve the rest of the recording. */ }
            return <span key={e.event_id}>{`[${e.occurred_at} ${e.data.stream}] ${text}\n`}</span>
          })}
        </pre>
        {files.map((e) => {
          const id = String(e.data.file_id)
          return <div key={id} className="text-xs space-y-1">
            <p className="break-all font-mono">{String(e.data.path)}</p>
            <button className="btn-ghost" onClick={async () => { setError(null); try { await downloadFile(data, id) } catch (err) { setError(err instanceof Error ? err.message : 'Ошибка скачивания') } }}>Скачать записанный файл</button>
          </div>
        })}
        {error && <p className="text-xs text-danger">{error}</p>}
      </>}
    </div>
  )
}
