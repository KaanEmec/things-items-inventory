import { formatDay } from '../format'

/** "In warranty until 14 Mar 2027" or "Warranty ended 3 Jan 2024"; null without an end date. */
export function warrantyLine(end: string | null | undefined, now = new Date()): { text: string; active: boolean } | null {
  if (!end) return null
  const day = end.slice(0, 10)
  const today = now.toISOString().slice(0, 10)
  const active = day >= today
  const d = formatDay(day)
  return { text: active ? `In warranty until ${d}` : `Warranty ended ${d}`, active }
}
