import { api, type Page } from '../api'
import type { Home, PlaceShort, TreePlace } from '../inventory/types'

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

export interface BoxDetail extends Box {
  items: { id: string; name: string; quantity: number }[]
  lines: { id: string; name: string; quantity: number }[]
}
export const getBoxDetail = (id: string) => api.get<BoxDetail>(`/api/v1/boxes/${encodeURIComponent(id)}`)
export const patchBox = (id: string, body: { goes_to_id?: string | null; handling?: Handling[]; note?: string | null; box_status?: BoxStatus }) =>
  api.patch<Box>(`/api/v1/boxes/${encodeURIComponent(id)}`, body)
export const bulkBoxes = (body: { ids: string[]; goes_to_id?: string | null; handling?: Handling[]; box_status?: BoxStatus }) =>
  api.post<{ data: Box[] }>('/api/v1/boxes/bulk', body)
/** Creates box `number`, or the next free one when omitted (a number below the next free is a 409). */
export const createBox = (number?: number) =>
  api.post<{ data: Box[] }>('/api/v1/boxes', number ? { from: number, to: number } : {})
export const unpackBox = (id: string, body: { moves: { item_id?: string; line_id?: string; place_id: string | null }[]; rest_to?: string }) =>
  api.post<Box>(`/api/v1/boxes/${encodeURIComponent(id)}/unpack`, body)

export interface Room {
  id: string
  name: string
  home: string
}
/** Rooms of every unarchived home: the choices for a box's destination. */
export async function listRooms(): Promise<Room[]> {
  const homes = (await api.get<Page<Home>>('/api/v1/homes')).data.filter((h) => !h.archived_at)
  const trees = await Promise.all(homes.map((h) => api.get<{ places: TreePlace[] }>(`/api/v1/homes/${h.id}/tree`)))
  const out: Room[] = []
  trees.forEach((t, i) => t.places.filter((p) => p.kind === 'room').forEach((p) => out.push({ id: p.id, name: p.name, home: homes[i].name })))
  return out
}
