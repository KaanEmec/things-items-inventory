import { expect, test } from 'vitest'
import { carryAttributes } from './categories'
import { ignoreShortcut, isTypingTarget, listAction } from './keys'
import { buildRows, searchPlaces } from './places'
import { fitWithin } from './photos'
import type { Category, TreePlace } from './types'
import { warrantyLine } from './warranty'
import { scrollToRow, windowRange } from './window'

const clothing: Category = {
  key: 'clothing',
  attributes: [
    { key: 'type', type: 'enum', values: ['top', 'shoes'] },
    { key: 'size', type: 'text' },
    { key: 'season', type: 'enum', values: ['summer', 'winter'] },
  ],
}

test('category change keeps attributes the new category also has and drops the rest', () => {
  const out = carryAttributes({ type: 'laptop', size: 'M', ram_gb: 16, season: 'winter', power: '65 W' }, clothing)
  // type=laptop is not a clothing type, ram_gb and power do not exist there
  expect(out).toEqual({ size: 'M', season: 'winter' })
})

test('carried attributes must still fit: enum values, multi lists, numbers', () => {
  const gadget: Category = {
    key: 'gadget',
    attributes: [
      { key: 'connectivity', type: 'multi', values: ['usb_c', 'wifi'] },
      { key: 'year', type: 'int' },
      { key: 'type', type: 'enum', values: ['phone'] },
    ],
  }
  expect(carryAttributes({ connectivity: ['wifi', 'x'], year: '2024', type: 'phone' }, gadget)).toEqual({ connectivity: ['wifi'], year: '2024', type: 'phone' })
  expect(carryAttributes({ connectivity: ['x'], year: 'abc' }, gadget)).toEqual({})
  expect(carryAttributes({ size: 'M' }, undefined)).toEqual({})
})

test('photos are scaled so the longest side is at most 2048 and never upscaled', () => {
  expect(fitWithin(4000, 3000)).toEqual({ width: 2048, height: 1536 })
  expect(fitWithin(3000, 4000)).toEqual({ width: 1536, height: 2048 })
  expect(fitWithin(2048, 100)).toEqual({ width: 2048, height: 100 })
  expect(fitWithin(800, 600)).toEqual({ width: 800, height: 600 })
  expect(fitWithin(10000, 1)).toEqual({ width: 2048, height: 1 })
})

test('list keys are ignored while typing, with modifiers or in a dialog', () => {
  const input = { tagName: 'INPUT', getAttribute: () => 'text' }
  expect(isTypingTarget(input as any)).toBe(true)
  expect(isTypingTarget({ tagName: 'TEXTAREA' } as any)).toBe(true)
  expect(isTypingTarget({ tagName: 'SELECT' } as any)).toBe(true)
  expect(isTypingTarget({ tagName: 'DIV', isContentEditable: true } as any)).toBe(true)
  expect(isTypingTarget({ tagName: 'INPUT', getAttribute: () => 'checkbox' } as any)).toBe(false)
  expect(isTypingTarget({ tagName: 'BUTTON' } as any)).toBe(false)
  expect(isTypingTarget(null)).toBe(false)
  expect(ignoreShortcut({ key: 'j', target: input as any })).toBe(true)
  expect(ignoreShortcut({ key: 'j', target: { tagName: 'BODY' } as any })).toBe(false)
  expect(ignoreShortcut({ key: 'j', target: { tagName: 'BODY' } as any, metaKey: true })).toBe(true)
  expect(ignoreShortcut({ key: 'j', target: { tagName: 'BODY' } as any }, true)).toBe(true)
  expect(listAction('j')).toBe('next')
  expect(listAction('K')).toBe('prev')
  expect(listAction('Enter')).toBe('open')
  expect(listAction('q')).toBeUndefined()
})

test('windowing renders only rows near the viewport', () => {
  expect(windowRange(0, 600, 56, 1000, 6)).toEqual({ start: 0, end: 17 })
  expect(windowRange(5600, 600, 56, 1000, 6)).toEqual({ start: 94, end: 117 })
  expect(windowRange(0, 600, 56, 3, 6)).toEqual({ start: 0, end: 3 })
  expect(scrollToRow(0, 600, 56, 20)).toBe(20 * 56 + 56 - 600)
  expect(scrollToRow(1000, 600, 56, 2)).toBe(112)
  expect(scrollToRow(0, 600, 56, 3)).toBe(0)
})

const p = (id: string, parent: string | null, name: string, sort = 0): TreePlace => ({ id, parent_id: parent, sort, kind: 'room', name, path: name, home_id: 'h', box_number: null, color: null })

test('place tree: depth-first rows in sort order, search by every word', () => {
  const rows = buildRows([p('b', 'a', 'Drawer', 2), p('a', null, 'Bedroom'), p('c', 'a', 'Closet', 1), p('z', null, 'Attic', 5)])
  expect(rows.map((r) => [r.place.id, r.depth])).toEqual([['a', 0], ['c', 1], ['b', 1], ['z', 0]])
  expect(rows[0].hasChildren).toBe(true)
  const places = [{ ...p('1', null, 'Drawer'), path: '3rd drawer · left closet · bedroom' }, p('2', null, 'Attic')]
  expect(searchPlaces(places, 'closet DRAWER').map((x) => x.id)).toEqual(['1'])
  expect(searchPlaces(places, '')).toEqual([])
})

test('warranty line', () => {
  const now = new Date('2026-11-10T10:00:00Z')
  expect(warrantyLine('2027-03-14', now)).toMatchObject({ active: true })
  expect(warrantyLine('2027-03-14', now)!.text).toMatch(/^In warranty until .*2027/)
  expect(warrantyLine('2024-01-03', now)).toMatchObject({ active: false })
  expect(warrantyLine('2024-01-03', now)!.text).toMatch(/^Warranty ended /)
  expect(warrantyLine(null, now)).toBeNull()
})
