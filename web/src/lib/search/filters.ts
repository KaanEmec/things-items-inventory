// Items-list filters <-> URL query string.
export type Sort = 'touched' | 'name' | 'place'
export interface Filters {
  category: string
  statuses: string[]
  placeId: string
  draft: boolean
  warranty: boolean
  sort: Sort
}
export const emptyFilters = (): Filters => ({ category: '', statuses: [], placeId: '', draft: false, warranty: false, sort: 'touched' })

const sorts: Sort[] = ['touched', 'name', 'place']

export function parseFilters(search: string): Filters {
  const p = new URLSearchParams(search)
  const sort = p.get('sort') as Sort
  return {
    category: p.get('category') ?? '',
    statuses: [...new Set(p.getAll('status').join(',').split(',').filter(Boolean))],
    placeId: p.get('place') ?? '',
    draft: p.get('draft') === '1',
    warranty: p.get('warranty') === '1',
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
  if (f.warranty) p.set('warranty', '1')
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
    warranty: f.warranty ? ('any' as const) : undefined,
    sort: f.sort,
  }
}
