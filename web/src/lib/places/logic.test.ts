import { expect, test } from 'vitest'
import { buildRows } from '../inventory/places'
import type { TreePlace } from '../inventory/types'
import {
  canDropItems,
  canDropPlace,
  childKind,
  countsAfterItemMove,
  dropPosition,
  edgeAt,
  expandSpots,
  initialExpanded,
  reorderTarget,
  toggled,
  treeKey,
  visibleRows,
} from './logic'

const names = (s: string) => expandSpots(s).names

test('spot patterns', () => {
  expect(names('drawer 1–4')).toEqual(['drawer 1', 'drawer 2', 'drawer 3', 'drawer 4'])
  expect(names('drawer 1-3')).toEqual(['drawer 1', 'drawer 2', 'drawer 3'])
  expect(names('Drawer 2 to 3')).toEqual(['Drawer 2', 'Drawer 3'])
  expect(names('shelf 08-10')).toEqual(['shelf 08', 'shelf 09', 'shelf 10'])
  expect(names('bin A-C')).toEqual(['bin A', 'bin B', 'bin C'])
  expect(names('drawer 1-2 left')).toEqual(['drawer 1 left', 'drawer 2 left'])
  expect(names('left, middle ,right')).toEqual(['left', 'middle', 'right'])
  expect(names('Top shelf')).toEqual(['Top shelf'])
  expect(names('  ')).toEqual([])
})

test('spot patterns refuse what makes no sense', () => {
  expect(expandSpots('drawer 4-1').error).toMatch(/count up/)
  expect(expandSpots('drawer 1-500').names).toEqual([])
  expect(expandSpots('drawer 1-500').error).toMatch(/up to/)
  expect(names('bin a-C')).toEqual(['bin a-C'])
  // a model number is not a range
  expect(names('USB-C hub')).toEqual(['USB-C hub'])
})

test('child kind follows the parent', () => {
  expect([null, 'floor', 'room', 'furniture', 'spot'].map((k) => childKind(k))).toEqual(['room', 'room', 'furniture', 'spot', 'spot'])
})

let n = 0
const place = (id: string, parent: string | null, sort = n++, over: Partial<TreePlace> = {}): TreePlace => ({
  id,
  kind: parent ? 'furniture' : 'room',
  name: id,
  path: id,
  home_id: 'h',
  box_number: null,
  color: null,
  parent_id: parent,
  sort,
  counts: { items: 0, items_below: 0, lines: 0 },
  ...over,
})
// bedroom > closet > (d1, d2); bedroom > desk; kitchen
const tree = [place('bedroom', null, 0), place('closet', 'bedroom', 0), place('d1', 'closet', 0), place('d2', 'closet', 1), place('desk', 'bedroom', 1), place('kitchen', null, 1)]

test('tree rows hide the children of collapsed nodes', () => {
  const rows = buildRows(tree)
  expect(visibleRows(rows, new Set()).map((r) => r.place.id)).toEqual(['bedroom', 'kitchen'])
  expect(visibleRows(rows, new Set(['bedroom'])).map((r) => r.place.id)).toEqual(['bedroom', 'closet', 'desk', 'kitchen'])
  expect(visibleRows(rows, new Set(['bedroom', 'closet'])).map((r) => r.place.id)).toEqual(['bedroom', 'closet', 'd1', 'd2', 'desk', 'kitchen'])
})

test('expand state: toggling and the way to the selected place', () => {
  expect([...toggled(new Set(['a']), 'b')]).toEqual(['a', 'b'])
  expect([...toggled(new Set(['a', 'b']), 'b')]).toEqual(['a'])
  expect([...toggled(new Set(), 'b', false)]).toEqual([])
  expect([...initialExpanded(tree, null)].sort()).toEqual(['bedroom', 'kitchen'])
  expect([...initialExpanded(tree, 'd1')].sort()).toEqual(['bedroom', 'closet', 'kitchen'])
})

test('tree keys', () => {
  const rows = buildRows(tree)
  const open = new Set(['bedroom'])
  const vis = visibleRows(rows, open)
  expect(treeKey(vis, open, 'bedroom', 'ArrowDown')).toEqual({ focus: 'closet' })
  expect(treeKey(vis, open, 'kitchen', 'ArrowDown')).toEqual({ focus: 'kitchen' })
  expect(treeKey(vis, open, 'closet', 'ArrowRight')).toEqual({ open: { id: 'closet', value: true } })
  expect(treeKey(vis, open, 'bedroom', 'ArrowRight')).toEqual({ focus: 'closet' })
  expect(treeKey(vis, open, 'bedroom', 'ArrowLeft')).toEqual({ open: { id: 'bedroom', value: false } })
  expect(treeKey(vis, open, 'desk', 'ArrowLeft')).toEqual({ focus: 'bedroom' })
  expect(treeKey(vis, open, 'closet', 'End')).toEqual({ focus: 'kitchen' })
  expect(treeKey(vis, open, 'closet', 'Enter')).toEqual({ select: 'closet' })
  expect(treeKey(vis, open, 'closet', 'ArrowDown', true)).toEqual({ reorder: { id: 'closet', step: 1 } })
  expect(treeKey(vis, open, 'closet', 'x')).toEqual({})
})

test('reorder steps stay inside the siblings', () => {
  expect(reorderTarget(tree, 'closet', 1)).toBe(1)
  expect(reorderTarget(tree, 'closet', -1)).toBeNull()
  expect(reorderTarget(tree, 'kitchen', 1)).toBeNull()
  expect(reorderTarget(tree, 'kitchen', -1)).toBe(0)
})

test('a place cannot be dropped on itself, inside itself, or where it already is', () => {
  expect(canDropPlace(tree, 'closet', 'closet', 'inside')).toBe(false)
  expect(canDropPlace(tree, 'bedroom', 'd1', 'inside')).toBe(false)
  expect(canDropPlace(tree, 'bedroom', 'closet', 'before')).toBe(false)
  expect(canDropPlace(tree, 'closet', 'bedroom', 'inside')).toBe(false) // already its parent
  expect(canDropPlace(tree, 'closet', 'kitchen', 'inside')).toBe(true)
  expect(canDropPlace(tree, 'd1', 'desk', 'inside')).toBe(true)
  expect(canDropPlace(tree, 'closet', 'desk', 'after')).toBe(true)
  expect(canDropPlace(tree, 'closet', 'desk', 'before')).toBe(false) // same slot
  expect(canDropPlace(tree, 'desk', 'closet', 'before')).toBe(true)
  expect(canDropPlace(tree, 'd2', 'bedroom', 'before')).toBe(true) // out to the top level
  expect(canDropPlace([...tree, place('box1', null, 5, { kind: 'box' })], 'closet', 'box1', 'inside')).toBe(false)
})

test('drop positions', () => {
  expect(dropPosition(tree, 'd1', 'desk', 'inside')).toEqual({ parent_id: 'desk', sort: 0 })
  expect(dropPosition(tree, 'desk', 'closet', 'before')).toEqual({ parent_id: 'bedroom', sort: 0 })
  expect(dropPosition(tree, 'closet', 'desk', 'after')).toEqual({ parent_id: 'bedroom', sort: 1 })
  expect(dropPosition(tree, 'd2', 'bedroom', 'after')).toEqual({ parent_id: null, sort: 1 })
  expect(edgeAt(2, 40)).toBe('before')
  expect(edgeAt(20, 40)).toBe('inside')
  expect(edgeAt(38, 40)).toBe('after')
})

test('items can be dropped unless they are all there already', () => {
  const at = (id: string | null) => ({ place: id ? ({ id } as never) : null })
  expect(canDropItems([at('d1')], 'd1')).toBe(false)
  expect(canDropItems([at('d1'), at('d2')], 'd1')).toBe(true)
  expect(canDropItems([at(null)], 'd1')).toBe(true)
  expect(canDropItems([], 'd1')).toBe(false)
})

test('counts follow moved items up the tree', () => {
  const counted = tree.map((p) => ({ ...p, counts: { items: p.id === 'd1' ? 3 : 0, items_below: ['d1', 'closet', 'bedroom'].includes(p.id) ? 3 : 0, lines: 0 } }))
  const at = (id: string) => ({ place: { id } as never })
  const out = countsAfterItemMove(counted, [at('d1'), at('d1')], 'kitchen')
  const c = (id: string) => out.find((p) => p.id === id)!.counts
  expect(c('d1')).toEqual({ items: 1, items_below: 1, lines: 0 })
  expect(c('closet')!.items_below).toBe(1)
  expect(c('bedroom')!.items_below).toBe(1)
  expect(c('kitchen')).toEqual({ items: 2, items_below: 2, lines: 0 })
  // same place: nothing changes
  expect(countsAfterItemMove(counted, [at('d1')], 'd1')).toBe(counted)
})
