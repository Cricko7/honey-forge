import test from 'node:test'
import assert from 'node:assert/strict'
import { eventSeverity, severityLabel } from './event-severity.ts'
import { createAlertTracker } from './alerts.ts'

test('all four severities remain distinct, including old notifications and event details', () => {
  for (const severity of ['low', 'medium', 'high', 'critical']) {
    assert.equal(eventSeverity({ event_type: 'http.custom', severity }), severity)
    assert.equal(eventSeverity({ event_type: 'http.custom', data: { severity } }), severity)
  }
  assert.equal(eventSeverity({ event_type: 'service.auth_attempt' }), 'high')
  assert.equal(eventSeverity({ event_type: 'tcp.connection_opened' }), 'low')
  assert.equal(eventSeverity({ event_type: 'http.custom', severity: 'urgent' }), 'low')
  assert.equal(eventSeverity({ event_type: 'http.block', data: { severity: 'critical' } }), 'low')
})

test('notifications carry the event severity and a visible importance label', () => {
  for (const severity of ['low', 'medium', 'high', 'critical']) {
    const [alert] = createAlertTracker().accept({ kind: 'event.created', event: {
      event_id: severity, event_type: 'http.custom', severity,
      source: { ip: '192.0.2.1', port: 123 }, destination: { protocol: 'http', port: 80 },
    } })
    assert.equal(alert.severity, severity)
    assert.ok(alert.title.includes(severityLabel(severity)))
    assert.equal(alert.level, severity === 'low' ? 'info' : severity === 'medium' ? 'warning' : 'error')
  }
})
