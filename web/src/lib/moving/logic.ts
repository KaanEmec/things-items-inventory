// Pure parts of the Moving screen: counts, room filter, number input.
import type { Box, BoxStatus } from './boxes'

export const STATUS_LABEL: Record<BoxStatus, string> = { empty: 'Empty', open: 'Open', packed: 'Packed', arrived: 'Arrived', unpacked: 'Unpacked' }

export interface Counts {
  total: number
  byStatus: Record<BoxStatus, number>
  /** Items and contents lines inside boxes. */
  items: number
  /** Boxes packed or further along, as a share of all boxes (0-100). */
  percent: number
}

const statusOf = (b: Box): BoxStatus => b.box_status ?? 'empty'

export function countBoxes(boxes: Box[]): Counts {
  const byStatus: Record<BoxStatus, number> = { empty: 0, open: 0, packed: 0, arrived: 0, unpacked: 0 }
  let items = 0
  for (const b of boxes) {
    byStatus[statusOf(b)]++
    items += b.counts?.items_below ?? 0
  }
  const done = byStatus.packed + byStatus.arrived + byStatus.unpacked
  return { total: boxes.length, byStatus, items, percent: boxes.length ? Math.round((done / boxes.length) * 100) : 0 }
}

export interface RoomChip {
  id: string
  name: string
  color: string | null
  count: number
}

/** One chip per destination room that has boxes, most boxes first. */
export function roomChips(boxes: Box[]): RoomChip[] {
  const m = new Map<string, RoomChip>()
  for (const b of boxes) {
    if (!b.goes_to) continue
    const c = m.get(b.goes_to.id) ?? { id: b.goes_to.id, name: b.goes_to.name, color: b.goes_to.color, count: 0 }
    c.count++
    m.set(b.goes_to.id, c)
  }
  return [...m.values()].sort((a, b) => b.count - a.count || a.name.localeCompare(b.name))
}

/** `room` is a room id, `''` for all, or `none` for boxes without a destination. */
export function filterBoxes(boxes: Box[], room: string): Box[] {
  if (!room) return boxes
  if (room === 'none') return boxes.filter((b) => !b.goes_to)
  return boxes.filter((b) => b.goes_to?.id === room)
}

/** "14", "#14" or "box 14" become 14; anything else is null. */
export function parseBoxNumber(text: string): number | null {
  const m = /^\s*(?:box\s*)?#?\s*(\d{1,6})\s*$/i.exec(text)
  const n = m ? Number(m[1]) : NaN
  return Number.isInteger(n) && n > 0 ? n : null
}

/** Per room: boxes packed (or further) out of all. For the Overview summary. */
export function roomProgress(boxes: Box[]): { room: RoomChip; packed: number }[] {
  return roomChips(boxes).map((room) => ({
    room,
    packed: boxes.filter((b) => b.goes_to?.id === room.id && ['packed', 'arrived', 'unpacked'].includes(statusOf(b))).length,
  }))
}

export interface BoxGroup {
  room: { id: string; name: string; color: string | null } | null
  boxes: Box[]
}

/** Boxes that are not unpacked yet (and not empty), grouped by destination room, rooms by name, "no room" last. */
export function inBoxes(boxes: Box[]): BoxGroup[] {
  const m = new Map<string, BoxGroup>()
  for (const b of boxes) {
    const s = b.box_status ?? 'empty'
    if (s === 'empty' || s === 'unpacked') continue
    const key = b.goes_to?.id ?? ''
    const g = m.get(key) ?? { room: b.goes_to, boxes: [] }
    g.boxes.push(b)
    m.set(key, g)
  }
  return [...m.values()].sort((a, b) => (a.room ? 0 : 1) - (b.room ? 0 : 1) || (a.room?.name ?? '').localeCompare(b.room?.name ?? ''))
}
