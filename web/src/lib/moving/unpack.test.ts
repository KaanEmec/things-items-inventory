import { expect, test } from 'vitest'
import { fillRest, toMoves, undecided, type Thing } from './unpack'

const things: Thing[] = [{ id: 'a', kind: 'item' }, { id: 'b', kind: 'line' }, { id: 'c', kind: 'item' }]

test('toMoves lists only what was decided', () => {
  const c = { c: { place: null }, a: { place: 'p1' } }
  expect(toMoves(things, c)).toEqual([{ item_id: 'a', place_id: 'p1' }, { item_id: 'c', place_id: null }])
  expect(undecided(things, c)).toBe(1)
})

test('fillRest keeps earlier choices', () => {
  const c = fillRest(things, { a: { place: null } }, 'room')
  expect(c.a.place).toBeNull()
  expect(c.b.place).toBe('room')
  expect(undecided(things, c)).toBe(0)
})
