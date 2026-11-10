import { expect, test } from 'vitest'
import type { Box } from './boxes'
import { countBoxes, filterBoxes, parseBoxNumber, roomChips, roomProgress } from './logic'

const room = (id: string, name: string) => ({ id, name, color: 'blue' })
const box = (n: number, status: Box['box_status'], goes_to: Box['goes_to'], items = 0): Box => ({
  id: `b${n}`, kind: 'box', name: `Box ${n}`, path: `Box ${n}`, home_id: null, box_number: n, color: null,
  goes_to, handling: [], box_status: status, counts: { items, items_below: items, lines: 0 },
})
const boxes = [box(1, 'packed', room('r1', 'Bedroom'), 5), box(2, 'open', room('r1', 'Bedroom'), 2), box(3, 'empty', room('r2', 'Kitchen')), box(4, null, null)]

test('countBoxes', () => {
  const c = countBoxes(boxes)
  expect(c.byStatus).toMatchObject({ packed: 1, open: 1, empty: 2 })
  expect(c.items).toBe(7)
  expect(c.percent).toBe(25)
})

test('room chips and filter', () => {
  expect(roomChips(boxes).map((c) => [c.name, c.count])).toEqual([['Bedroom', 2], ['Kitchen', 1]])
  expect(filterBoxes(boxes, 'r2').map((b) => b.box_number)).toEqual([3])
  expect(filterBoxes(boxes, 'none').map((b) => b.box_number)).toEqual([4])
  expect(filterBoxes(boxes, '')).toHaveLength(4)
  expect(roomProgress(boxes)[0].packed).toBe(1)
})

test('parseBoxNumber', () => {
  expect(parseBoxNumber('14')).toBe(14)
  expect(parseBoxNumber(' #14 ')).toBe(14)
  expect(parseBoxNumber('Box 7')).toBe(7)
  expect(parseBoxNumber('0')).toBeNull()
  expect(parseBoxNumber('x')).toBeNull()
})

import { inBoxes } from './logic'
test('inBoxes groups unfinished boxes by room', () => {
  const g = inBoxes([...boxes, box(5, 'arrived', room('r2', 'Kitchen')), box(6, 'unpacked', room('r2', 'Kitchen'))])
  expect(g.map((x) => [x.room?.name ?? null, x.boxes.map((b) => b.box_number)])).toEqual([['Bedroom', [1, 2]], ['Kitchen', [5]]])
})
