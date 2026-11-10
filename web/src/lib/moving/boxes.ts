import { api, type Page } from '../api'
import type { PlaceShort } from '../inventory/types'

export type Handling = 'heavy' | 'fragile' | 'soft' | 'this_side_up'
export type BoxStatus = 'empty' | 'open' | 'packed' | 'arrived' | 'unpacked'

export interface GoesTo {
  id: string
  name: string
  /** A colour name from `room_colors` (red, blue, ...). */
  color: string | null
}

/** A place of kind `box` as GET /boxes sends it. */
export interface Box extends PlaceShort {
  goes_to: GoesTo | null
  handling: Handling[]
  box_status: BoxStatus | null
  note?: string | null
  /** Set by the server in the JSON; there is no way to write it yet. */
  label_printed_at?: string | null
  counts?: { items: number; items_below: number; lines: number }
}

export interface BoxFilter {
  status?: BoxStatus | ''
  goes_to_id?: string
}

export const getBox = (id: string) => api.get<Box>(`/api/v1/boxes/${encodeURIComponent(id)}`)
export const getPlace = (id: string) => api.get<Box>(`/api/v1/places/${encodeURIComponent(id)}`)

/** Every box, all pages, in number order. */
export async function listBoxes(filter: BoxFilter = {}): Promise<Box[]> {
  const out: Box[] = []
  let cursor: string | null = null
  do {
    const q = new URLSearchParams({ limit: '200' })
    if (filter.status) q.set('status', filter.status)
    if (filter.goes_to_id) q.set('goes_to_id', filter.goes_to_id)
    if (cursor) q.set('cursor', cursor)
    const page: Page<Box> = await api.get<Page<Box>>(`/api/v1/boxes?${q}`)
    out.push(...page.data)
    cursor = page.next_cursor
  } while (cursor)
  return out
}
