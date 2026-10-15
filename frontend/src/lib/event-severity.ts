export type EventSeverity = 'low' | 'medium' | 'high' | 'critical'

export function severityLabel(severity: EventSeverity): string {
  return { low: 'Обычное', medium: 'Средняя важность', high: 'Высокая важность', critical: 'Критическое' }[severity]
}

export function eventSeverity(event: { event_type: string; severity?: string; data?: Record<string, unknown> }): EventSeverity {
  const severity = event.severity ?? (event.event_type === 'http.custom' ? event.data?.severity : undefined)
  if (severity === 'low' || severity === 'medium' || severity === 'high' || severity === 'critical') return severity
  // Older journal notifications and full event details have no summary severity.
  return ['service.auth_attempt', 'ssh.auth_attempt', 'service.action', 'honeytoken.triggered'].includes(event.event_type) ? 'high' : 'low'
}
