const pad = (value: number) => String(value).padStart(2, '0')

export function localDateTime(date: Date): string {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

export function calendarDays(year: number, month: number): Date[] {
  const first = new Date(year, month, 1, 12)
  const offset = (first.getDay() + 6) % 7
  return Array.from({ length: 42 }, (_, index) => new Date(year, month, 1 - offset + index, 12))
}
