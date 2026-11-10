// Items-list filters <-> URL query string.
export type Sort = 'touched' | 'name' | 'place'
export interface Filters {
  category: string
  statuses: string[]
  placeId: string
  draft: boolean
  warranty: boolean
  /** With `warranty`: '' = any warranty, 'active' = still running, 'ending' = ends within 60 days (Overview links). */
  warrantyMode: '' | 'active' | 'ending'
  sort: Sort
}
export const emptyFilters = (): Filters => ({ category: '', statuses: [], placeId: '', draft: false, warranty: false, warrantyMode: '', sort: 'touched' })

const sorts: Sort[] = ['touched', 'name', 'place']

export function parseFilters(search: string): Filters {
  const p = new URLSearchParams(search)
  const sort = p.get('sort') as Sort
  const w = p.get('warranty')
  return {
    category: p.get('category') ?? '',
    statuses: [...new Set(p.getAll('status').join(',').split(',').filter(Boolean))],
    placeId: p.get('place') ?? '',
    draft: p.get('draft') === '1',
    warranty: w === '1' || w === 'active' || w === 'ending',
    warrantyMode: w === 'active' || w === 'ending' ? w : '',
    sort: sorts.includes(sort) ? sort : 'touched',
  }
}

/** Query string with a leading `?`, or '' when everything is at its default. */
export function filtersToSearch(f: Filters): string {
  const p = new URLSearchParams()
  if (f.category) p.set('category', f.category)
  if (f.statuses.length) p.set('status', f.statuses.join(','))
  if (f.placeId) p.set('place', f.placeId)
  if (f.draft) p.set('draft', '1')
  if (f.warranty) p.set('warranty', f.warrantyMode || '1')
  if (f.sort !== 'touched') p.set('sort', f.sort)
  const s = p.toString()
  return s ? `?${s}` : ''
}

export const filterCount = (f: Filters) => +!!f.category + +!!f.statuses.length + +!!f.placeId + +f.draft + +f.warranty

/** Arguments for the items API. */
export function toQuery(f: Filters) {
  return {
    category: f.category || undefined,
    status: f.statuses,
    place_id: f.placeId || undefined,
    below: true,
    draft: f.draft || undefined,
    warranty: f.warranty ? f.warrantyMode || ('any' as const) : undefined,
    sort: f.sort,
  }
}
