import { expect, test } from 'vitest'
import { buildPrintUrl, clampCopies, defaultRequest, formatRange, layout, paginate, parsePrintUrl, parseRange, patternFor, roomTint, sheetsNeeded } from './print'

test('parseRange reads numbers and ranges', () => {
  expect(parseRange('3-7, 12').numbers).toEqual([3, 4, 5, 6, 7, 12])
  expect(parseRange('12, 3-5 4').numbers).toEqual([3, 4, 5, 12])
  expect(parseRange('').numbers).toEqual([])
})

test('parseRange reports what it cannot read', () => {
  const r = parseRange('2, x, 9-4, 0')
  expect(r.numbers).toEqual([2])
  expect(r.invalid).toEqual(['x', '9-4', '0'])
})

test('formatRange compresses', () => {
  expect(formatRange([12, 3, 4, 5, 6, 7])).toBe('3-7,12')
  expect(formatRange([])).toBe('')
})

test('three box labels fit a sheet of A4 and Letter', () => {
  expect(layout('a4', 'box').perSheet).toBe(3)
  expect(layout('letter', 'box').perSheet).toBe(3)
  expect(layout('a4', 'place').perSheet).toBeGreaterThanOrEqual(10)
})

test('sheetsNeeded', () => {
  expect(sheetsNeeded(0, 3, 0, 'a4')).toBe(0)
  expect(sheetsNeeded(1, 3, 0, 'a4')).toBe(1)
  expect(sheetsNeeded(40, 3, 0, 'a4')).toBe(40)
  expect(sheetsNeeded(5, 4, 0, 'letter')).toBe(7)
  const p = layout('a4', 'place').perSheet
  expect(sheetsNeeded(1, 3, p + 1, 'a4')).toBe(1 + 2)
})

test('copies are clamped to 1-4', () => {
  expect([0, 9, 2.4, NaN].map(clampCopies)).toEqual([1, 4, 2, 3])
})

test('paginate', () => {
  expect(paginate([1, 2, 3, 4, 5], 2)).toEqual([[1, 2], [3, 4], [5]])
})

test('print URL round trip', () => {
  const r = { boxes: [3, 4, 5, 12], places: ['p1', 'p2'], copies: 2, paper: 'letter' as const, mono: true, qr: true }
  const url = buildPrintUrl(r)
  expect(url).toBe('/print/labels?boxes=3-5%2C12&places=p1%2Cp2&copies=2&paper=letter&mono=1&qr=1')
  expect(parsePrintUrl(url.split('?')[1])).toEqual(r)
  expect(parsePrintUrl('')).toEqual(defaultRequest())
  expect(parsePrintUrl('?paper=x&copies=99').copies).toBe(4)
})

test('room tint and pattern', () => {
  expect(roomTint('blue').fg).toBe('#FFFFFF')
  expect(roomTint(null).bg).toBe('#E7E6E0')
  expect(roomTint('#FFFF00').fg).toBe('#1A1A19')
  expect(patternFor('Kitchen')).toBe(patternFor('kitchen'))
})
