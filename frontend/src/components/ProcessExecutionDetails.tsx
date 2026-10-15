export function ProcessExecutionDetails({ data }: { data: Record<string, unknown> }) {
  const rows = [
    ['Программа', data.binary],
    ['Рабочий каталог', data.cwd],
    ['PID / PPID / UID', `${data.pid ?? '—'} / ${data.ppid ?? '—'} / ${data.uid ?? '—'}`],
    ['Сенсор', `${data.sensor ?? '—'} ${data.sensor_version ?? ''}`],
    ['Хост', data.host_id],
    ['Поколение развёртывания', data.generation],
    ['Контейнер', data.container_id],
    ['Запуск контейнера (UTC)', data.container_started_at],
  ]
  return (
    <div className="space-y-2">
      <dl className="divide-y divide-line text-sm">
        {rows.map(([label, value]) => (
          <div key={String(label)} className="py-2.5">
            <dt className="text-xs text-muted">{String(label)}</dt>
            <dd className="mt-1 break-all font-mono text-xs">{String(value ?? '—')}</dd>
          </div>
        ))}
      </dl>
      <p className="text-xs text-muted">Аргументы не собираются. IP источника относится к SSH-соединению шлюза. Встроенные команды shell могут не создавать процесс.</p>
    </div>
  )
}
