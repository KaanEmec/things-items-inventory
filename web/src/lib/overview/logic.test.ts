import { describe, expect, it } from 'vitest'
import { daysUntil, entryHref, isFirstDay, phrase, shortAgo, stripCells, visibleGroups } from './logic'

const e = (kind: string, summary: Record<string, unknown> = {}, extra: object = {}) => ({ kind, item_name: 'Drone', place_name: null, summary, ...extra })

describe('phrase', () => {
  it('phrases a move with its destination', () => {
    expect(phrase(e('moved', { to: 'Left drawer · TV cabinet' }))).toEqual({ verb: 'moved', target: 'Drone', tail: 'to Left drawer · TV cabinet' })
  })
  it('handles a move out of every place', () => {
    expect(phrase(e('moved', { to: null })).tail).toBe('out of its place')
  })
  it('phrases status changes with a readable status', () => {
    expect(phrase(e('status_changed', { to: 'to_sell' }))).toEqual({ verb: 'marked', target: 'Drone', tail: 'as To sell / donate' })
  })
  it('lists edited fields, shortened', () => {
    expect(phrase(e('edited', { fields: ['brand', 'model', 'serial_no', 'color'] })).tail).toBe('(brand, model, serial no, …)')
  })
  it('falls back to the summary name and a generic noun', () => {
    expect(phrase({ kind: 'created', item_name: null, place_name: null, summary: { subject: 'item', name: 'Lamp' } }).target).toBe('Lamp')
    expect(phrase({ kind: 'deleted', item_name: null, place_name: null, summary: { subject: 'item' } }).target).toBe('an item')
    expect(phrase({ kind: 'packed', item_name: null, place_name: 'Box 4', summary: { subject: 'box' } }).target).toBe('Box 4')
  })
  it('does not throw on unknown kinds', () => {
    expect(phrase(e('weird')).verb).toBe('changed')
  })
})

describe('time', () => {
  const now = Date.parse('2026-11-10T12:00:00Z')
  it('shortens ages', () => {
    expect(shortAgo('2026-11-10T11:59:40Z', now)).toBe('now')
    expect(shortAgo('2026-11-10T11:30:00Z', now)).toBe('30 min')
    expect(shortAgo('2026-11-10T07:00:00Z', now)).toBe('5 h')
    expect(shortAgo('2026-11-07T12:00:00Z', now)).toBe('3 d')
    expect(shortAgo('2026-10-13T12:00:00Z', now)).toBe('4 wk')
    expect(shortAgo('2026-04-10T12:00:00Z', now)).toBe('7 mo')
    expect(shortAgo('2024-11-10T12:00:00Z', now)).toBe('2 y')
    expect(shortAgo(null, now)).toBe('')
  })
  it('counts days until a date', () => {
    expect(daysUntil('2026-12-03', now)).toBe(23)
    expect(daysUntil('2026-11-10', now)).toBe(0)
    expect(daysUntil('2026-11-08', now)).toBe(-2)
  })
})

describe('links and cells', () => {
  it('links to the item or the place', () => {
    expect(entryHref({ item_id: 'a', place_id: 'b' })).toBe('/items/a')
    expect(entryHref({ item_id: null, place_id: 'b c' })).toBe('/places?place=b%20c')
    expect(entryHref({ item_id: null, place_id: null })).toBeNull()
  })
  it('builds strip cells whose URLs the Items list understands', () => {
    const cells = stripCells({ all: 5, drafts: 1, packed: 2, to_sell: 3, in_warranty: 4 })
    expect(cells.map((c) => c.value)).toEqual([5, 1, 2, 3, 4])
    expect(cells.map((c) => c.href)).toEqual(['/items', '/items?draft=1', '/items?status=packed', '/items?status=to_sell', '/items?warranty=active'])
  })
  it('hides empty groups and detects the first day', () => {
    expect(visibleGroups([{ kind: 'drafts', count: 0, rows: [] }, { kind: 'unseen', count: 2, rows: [] }]).map((g) => g.kind)).toEqual(['unseen'])
    const z = { all: 0, drafts: 0, packed: 0, to_sell: 0, in_warranty: 0 }
    expect(isFirstDay(z, 0)).toBe(true)
    expect(isFirstDay(z, 3)).toBe(false)
    expect(isFirstDay({ ...z, all: 1 }, 0)).toBe(false)
  })
})
