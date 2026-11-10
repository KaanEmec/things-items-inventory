// Non-visual logic of the Places screen: spot patterns, tree rows and expand state,
// keyboard moves, drop targets, optimistic counts.
import { ancestorIds, type TreeRow } from '../inventory/places'
import type { Item, TreePlace } from '../inventory/types'

// ---- "drawer 1–4" ----------------------------------------------------------

export const MAX_SPOTS = 60
const dash = '(?:-|–|—|\\.\\.+|\\bto\\b|\\bbis\\b)'
const range = new RegExp(`^(.*?)(\\d+|[A-Za-z])\\s*${dash}\\s*(\\d+|[A-Za-z])(?!\\w)(.*)$`, 'i')

/**
 * Names for a pattern: `drawer 1–4` gives drawer 1 … drawer 4, `shelf 01-03` keeps the zeros,
 * `bin A-D` runs letters, `left, middle, right` splits a list, anything else is one name.
 * A range may have text after it (`drawer 1-3 left`).
 */
export function expandSpots(input: string): { names: string[]; error: string } {
  const text = input.trim()
  if (!text) return { names: [], error: '' }
  if (text.includes(',') || text.includes(';') || text.includes('\n')) {
    const names = text.split(/[,;\n]/).map((s) => s.trim()).filter(Boolean)
    return names.length > MAX_SPOTS ? { names: [], error: `That makes ${names.length} places; add up to ${MAX_SPOTS} at once.` } : { names, error: '' }
  }
  const m = range.exec(text)
  if (!m) return { names: [text], error: '' }
  const [, head, a, b, tail] = m
  const num = /^\d+$/.test(a) && /^\d+$/.test(b)
  const letters = /^[A-Za-z]$/.test(a) && /^[A-Za-z]$/.test(b)
  if ((!num && !letters) || (letters && head !== '' && !/\s$/.test(head))) return { names: [text], error: '' }
  let from: number, to: number
  if (num) (from = Number(a)), (to = Number(b))
  else (from = a.charCodeAt(0)), (to = b.charCodeAt(0))
  if (letters && (a === a.toLowerCase()) !== (b === b.toLowerCase())) return { names: [text], error: '' }
  if (to < from) return { names: [], error: 'The range must count up, like 1–4.' }
  const count = to - from + 1
  if (count > MAX_SPOTS) return { names: [], error: `That makes ${count} places; add up to ${MAX_SPOTS} at once.` }
  const pad = num && a.length > 1 && a.startsWith('0') ? a.length : 0
  const names: string[] = []
  for (let n = from; n <= to; n++) {
    const label = num ? String(n).padStart(pad, '0') : String.fromCharCode(n)
    names.push(`${head}${label}${tail}`.replace(/\s+/g, ' ').trim())
  }
  return { names, error: '' }
}

/** Kind of a new place inside `parent` (null = top of the home). */
export function childKind(parentKind: string | null | undefined): 'floor' | 'room' | 'furniture' | 'spot' {
  switch (parentKind) {
    case 'floor':
      return 'room'
    case 'room':
      return 'furniture'
    case 'furniture':
    case 'spot':
      return 'spot'
    default:
      return 'room'
  }
}

export const kindWord = (kind: string, n = 1) => {
  const w = kind === 'furniture' ? 'furniture' : kind
  return n === 1 || w === 'furniture' ? w : `${w}s`
}

// ---- tree rows and expand state -------------------------------------------

/** Rows that show: children of a collapsed node are skipped. */
export function visibleRows(rows: TreeRow[], expanded: ReadonlySet<string>): TreeRow[] {
  const out: TreeRow[] = []
  let skip = -1
  for (const r of rows) {
    if (skip >= 0 && r.depth > skip) continue
    skip = -1
    out.push(r)
    if (r.hasChildren && !expanded.has(r.place.id)) skip = r.depth
  }
  return out
}

export function toggled(set: ReadonlySet<string>, id: string, open?: boolean): Set<string> {
  const next = new Set(set)
  const want = open ?? !next.has(id)
  if (want) next.add(id)
  else next.delete(id)
  return next
}

/** What is open when the tree first shows: the top level and the way to the selected place. */
export function initialExpanded(places: TreePlace[], selectedId: string | null | undefined): Set<string> {
  const byId = new Map(places.map((p) => [p.id, p]))
  const open = new Set<string>()
  for (const p of places) if (!p.parent_id || !byId.has(p.parent_id)) open.add(p.id)
  for (const id of ancestorIds(places, selectedId)) open.add(id)
  return open
}

export interface TreeKeyResult {
  /** Move the keyboard focus here. */
  focus?: string
  /** Open or close this node. */
  open?: { id: string; value: boolean }
  /** Select (open on screen) this place. */
  select?: string
  /** Move the place one step among its siblings. */
  reorder?: { id: string; step: -1 | 1 }
}

/** What a key does in the tree (WAI-ARIA tree pattern, plus Alt+↑/↓ to reorder). */
export function treeKey(visible: TreeRow[], expanded: ReadonlySet<string>, focusId: string | null, key: string, alt = false): TreeKeyResult {
  if (!visible.length) return {}
  const at = Math.max(0, visible.findIndex((r) => r.place.id === focusId))
  const cur = visible[at]
  const id = cur.place.id
  switch (key) {
    case 'ArrowDown':
      return alt ? { reorder: { id, step: 1 } } : { focus: visible[Math.min(at + 1, visible.length - 1)].place.id }
    case 'ArrowUp':
      return alt ? { reorder: { id, step: -1 } } : { focus: visible[Math.max(at - 1, 0)].place.id }
    case 'Home':
      return { focus: visible[0].place.id }
    case 'End':
      return { focus: visible[visible.length - 1].place.id }
    case 'ArrowRight':
      if (cur.hasChildren && !expanded.has(id)) return { open: { id, value: true } }
      if (cur.hasChildren && visible[at + 1]?.depth > cur.depth) return { focus: visible[at + 1].place.id }
      return {}
    case 'ArrowLeft': {
      if (cur.hasChildren && expanded.has(id)) return { open: { id, value: false } }
      for (let i = at - 1; i >= 0; i--) if (visible[i].depth < cur.depth) return { focus: visible[i].place.id }
      return {}
    }
    case 'Enter':
    case ' ':
      return { select: id }
  }
  return {}
}

// ---- siblings, reorder, drop targets --------------------------------------

const keyOf = (p: TreePlace, ids: Set<string>) => (p.parent_id && ids.has(p.parent_id) ? p.parent_id : null)

/** Siblings of a place in tree order (including itself). */
export function siblingsOf(places: TreePlace[], id: string): TreePlace[] {
  const ids = new Set(places.map((p) => p.id))
  const me = places.find((p) => p.id === id)
  if (!me) return []
  const k = keyOf(me, ids)
  return places.filter((p) => keyOf(p, ids) === k).sort((a, b) => a.sort - b.sort || a.name.localeCompare(b.name))
}

/** New `sort` (position among the siblings) after moving a place one step up or down; null at the ends. */
export function reorderTarget(places: TreePlace[], id: string, step: -1 | 1): number | null {
  const sibs = siblingsOf(places, id)
  const i = sibs.findIndex((p) => p.id === id)
  const to = i + step
  return i < 0 || to < 0 || to >= sibs.length ? null : to
}

/** True when `id` is `ancestor` or sits somewhere below it. */
export function isWithin(places: TreePlace[], id: string, ancestor: string): boolean {
  const byId = new Map(places.map((p) => [p.id, p]))
  const seen = new Set<string>()
  for (let cur = byId.get(id); cur && !seen.has(cur.id); cur = cur.parent_id ? byId.get(cur.parent_id) : undefined) {
    if (cur.id === ancestor) return true
    seen.add(cur.id)
  }
  return false
}

export type Edge = 'before' | 'inside' | 'after'

/** Zone of a node under the pointer: the outer quarters reorder, the middle moves inside. */
export function edgeAt(offsetY: number, height: number): Edge {
  const r = height > 0 ? offsetY / height : 0.5
  return r < 0.25 ? 'before' : r > 0.75 ? 'after' : 'inside'
}

/**
 * Can place `moving` be dropped on `target` at `edge`? Not onto itself or something inside it,
 * not into a box, and not where it already is.
 */
export function canDropPlace(places: TreePlace[], moving: string, target: string, edge: Edge): boolean {
  if (moving === target) return false
  const t = places.find((p) => p.id === target)
  const m = places.find((p) => p.id === moving)
  if (!t || !m || isWithin(places, target, moving)) return false
  if (edge === 'inside') return t.kind !== 'box' && m.parent_id !== target
  if (t.parent_id) {
    const parent = places.find((p) => p.id === t.parent_id)
    if (parent?.kind === 'box') return false
  }
  const to = dropPosition(places, moving, target, edge)
  const sibs = siblingsOf(places, moving)
  return !(to.parent_id === (m.parent_id ?? null) && sibs.findIndex((p) => p.id === moving) === to.sort)
}

/** Where a drop at `edge` of `target` puts the moving place: parent (null = top of the home) and index among siblings. */
export function dropPosition(places: TreePlace[], moving: string, target: string, edge: Edge): { parent_id: string | null; sort: number } {
  const t = places.find((p) => p.id === target)!
  if (edge === 'inside') {
    const kids = places.filter((p) => p.parent_id === target && p.id !== moving)
    return { parent_id: target, sort: kids.length }
  }
  const sibs = siblingsOf(places, target).filter((p) => p.id !== moving)
  const i = sibs.findIndex((p) => p.id === target)
  return { parent_id: t.parent_id, sort: i + (edge === 'after' ? 1 : 0) }
}

/** Can the items be dropped on this place? Not when they all are there already. */
export function canDropItems(items: Pick<Item, 'place'>[], target: string): boolean {
  return items.length > 0 && items.some((i) => i.place?.id !== target)
}

// ---- optimistic counts -----------------------------------------------------

/** Tree with the counts of the places moved items left and joined adjusted, ancestors included. */
export function countsAfterItemMove(places: TreePlace[], items: Pick<Item, 'place'>[], target: string): TreePlace[] {
  const delta = new Map<string, { direct: number; below: number }>()
  const byId = new Map(places.map((p) => [p.id, p]))
  const bump = (id: string, direct: number, n: number) => {
    const seen = new Set<string>()
    for (let cur = byId.get(id), first = true; cur && !seen.has(cur.id); cur = cur.parent_id ? byId.get(cur.parent_id) : undefined, first = false) {
      seen.add(cur.id)
      const d = delta.get(cur.id) ?? { direct: 0, below: 0 }
      if (first) d.direct += direct
      d.below += n
      delta.set(cur.id, d)
    }
  }
  let moved = 0
  for (const it of items) {
    if (it.place?.id === target) continue
    moved++
    if (it.place?.id) bump(it.place.id, -1, -1)
  }
  if (!moved) return places
  bump(target, moved, moved)
  return places.map((p) => {
    const d = delta.get(p.id)
    return d && p.counts ? { ...p, counts: { ...p.counts, items: p.counts.items + d.direct, items_below: p.counts.items_below + d.below } } : p
  })
}

/** Ids of places that sit in the way of a message like "3 places, 2 items": the counts of a 409 on delete. */
export function notEmptyCounts(fields: Record<string, string> | undefined): { places: number; items: number; lines: number } {
  const n = (k: string) => Number(fields?.[k]) || 0
  return { places: n('places'), items: n('items'), lines: n('lines') }
}
