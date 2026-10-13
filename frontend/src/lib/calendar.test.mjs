import test from 'node:test'
import assert from 'node:assert/strict'
import { calendarDays, localDateTime } from './calendar.ts'

test('calendar begins on Monday, covers leap days and crosses years', () => {
  const days = calendarDays(2024, 1)
  assert.equal(days.length, 42)
  assert.equal(days[0].getDay(), 1)
  assert.equal(days.filter((day) => day.getMonth() === 1).length, 29)
  assert.equal(calendarDays(2026, 0)[0].getFullYear(), 2025)
})
test('date values retain local time without UTC conversion', () => {
  assert.equal(localDateTime(new Date(2026, 9, 13, 9, 5)), '2026-10-13T09:05')
})
