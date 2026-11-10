// Pure helpers for the Overview screen.
import type { Counts, NeedsGroup, NeedsKind, RecentEntry } from './types'

const statusLabels: Record<string, string> = { in_use: 'In use', stored: 'Stored', packed: 'Packed', to_sell: 'To sell / donate', lent: 'Lent out', gone: 'Gone' }
const statusLabel = (s: unknown) => (typeof s === 'string' ? (statusLabels[s] ?? s.replace(/_/g, ' ')) : 'another status')

export interface Phrase {
  /** The actor-less verb phrase: "moved". */
  verb: string
  /** What it happened to (an item, place or box name). */
  target: string
  /** What follows the target: "to Left drawer · TV cabinet". */
  tail: string
}

/** Phrases a history entry, e.g. `moved` / `DJI Mini 4 Pro` / `to Left drawer`. Never throws on odd data. */
export function phrase(e: Pick<RecentEntry, 'kind' | 'item_name' | 'place_name' | 'summary'>): Phrase {
  const s = e.summary ?? {}
  const subject = typeof s.subject === 'string' ? s.subject : 'item'
  const name = e.item_name ?? (typeof s.name === 'string' ? s.name : null) ?? e.place_name
  const target = subject === 'lines' ? 'contents lines' : (name ?? (subject === 'item' ? 'an item' : `a ${subject}`))
  const p = (verb: string, tail = ''): Phrase => ({ verb, target, tail })
  switch (e.kind) {
    case 'created':
      return p('added', typeof s.count === 'number' && s.count > 1 ? `(${s.count})` : '')
    case 'edited': {
      const f = Array.isArray(s.fields) ? s.fields.filter((x): x is string => typeof x === 'string') : []
      return p('edited', f.length ? `(${f.slice(0, 3).map((x) => x.replace(/_/g, ' ')).join(', ')}${f.length > 3 ? ', …' : ''})` : '')
    }
    case 'moved':
      return p('moved', typeof s.to === 'string' && s.to ? `to ${s.to}` : 'out of its place')
    case 'status_changed':
      return p('marked', `as ${statusLabel(s.to)}`)
    case 'photo_added':
      return p('added a photo to')
    case 'confirmed':
      return p('confirmed')
    case 'seen':
      return p('checked')
    case 'packed':
      return p('packed')
    case 'opened':
      return p('opened')
    case 'arrived':
      return p('marked arrived:')
    case 'unpacked':
      return p('unpacked')
    case 'deleted':
      return p('deleted')
    case 'restored':
      return p('restored')
    case 'settings_changed':
      return p('changed settings of')
    default:
      return p('changed')
  }
}

/** Where a recent change links to. */
export function entryHref(e: Pick<RecentEntry, 'item_id' | 'place_id'>): string | null {
  if (e.item_id) return `/items/${e.item_id}`
  if (e.place_id) return `/places?place=${encodeURIComponent(e.place_id)}`
  return null
}

/** Compact age for dense rows: "now", "5 min", "3 h", "2 d", "4 wk", "7 mo", "2 y". */
export function shortAgo(iso: string | null | undefined, now = Date.now()): string {
  if (!iso) return ''
  const s = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000))
  if (s < 60) return 'now'
  if (s < 3600) return `${Math.floor(s / 60)} min`
  if (s < 86400) return `${Math.floor(s / 3600)} h`
  if (s < 86400 * 14) return `${Math.floor(s / 86400)} d`
  if (s < 86400 * 60) return `${Math.floor(s / (86400 * 7))} wk`
  if (s < 86400 * 365) return `${Math.floor(s / (86400 * 30))} mo`
  return `${Math.floor(s / (86400 * 365))} y`
}

/** Whole days from `now` until the date (YYYY-MM-DD or ISO); negative when past. */
export function daysUntil(date: string, now = Date.now()): number {
  const d = Date.UTC(+date.slice(0, 4), +date.slice(5, 7) - 1, +date.slice(8, 10))
  const t = new Date(now)
  return Math.round((d - Date.UTC(t.getUTCFullYear(), t.getUTCMonth(), t.getUTCDate())) / 86400000)
}

export interface Cell {
  key: keyof Counts
  label: string
  value: number
  href: string
}

/** The status strip: label, number and the Items URL that lists exactly those items. */
export function stripCells(c: Counts): Cell[] {
  return [
    { key: 'all', label: 'All items', value: c.all, href: '/items' },
    { key: 'drafts', label: 'Drafts', value: c.drafts, href: '/items?draft=1' },
    { key: 'packed', label: 'Packed', value: c.packed, href: '/items?status=packed' },
    { key: 'to_sell', label: 'To sell / donate', value: c.to_sell, href: '/items?status=to_sell' },
    { key: 'in_warranty', label: 'In warranty', value: c.in_warranty, href: '/items?warranty=active' },
  ]
}

/** Title and "see all" link of a needs-a-look group. */
export const groupMeta: Record<NeedsKind, { title: (n: number) => string; href?: string }> = {
  drafts: { title: (n) => `${n} ${n === 1 ? 'draft waits' : 'drafts wait'} for review`, href: '/items?draft=1' },
  warranty_ending: { title: (n) => `${n} ${n === 1 ? 'warranty ends' : 'warranties end'} within 60 days`, href: '/items?warranty=ending' },
  unseen: { title: (n) => `${n} daily-use ${n === 1 ? 'item' : 'items'} not seen for 6 months` },
  failed_jobs: { title: (n) => `${n} AI ${n === 1 ? 'job' : 'jobs'} failed`, href: '/settings/ai' },
}

/** Only the groups that have something in them. */
export const visibleGroups = (g: NeedsGroup[]) => g.filter((x) => x.count > 0)

/** The empty-household state: nothing to count yet. */
export const isFirstDay = (counts: Counts, places: number) => counts.all === 0 && places === 0
