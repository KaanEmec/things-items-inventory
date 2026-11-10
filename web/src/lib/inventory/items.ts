import { api, type Page } from '../api'
import type { Item } from './types'

export interface ItemQuery {
  category?: string
  status?: string[]
  place_id?: string
  below?: boolean
  draft?: boolean
  warranty?: 'active' | 'ending' | 'any'
  sort?: 'touched' | 'name' | 'place'
}

export function itemsPath(q: ItemQuery, cursor?: string | null, limit = 100): string {
  const p = new URLSearchParams()
  if (q.category) p.set('category', q.category)
  for (const s of q.status ?? []) p.append('status', s)
  if (q.place_id) {
    p.set('place_id', q.place_id)
    if (q.below) p.set('below', '1')
  }
  if (q.draft) p.set('draft', '1')
  if (q.warranty) p.set('warranty', q.warranty)
  if (q.sort) p.set('sort', q.sort)
  if (cursor) p.set('cursor', cursor)
  p.set('limit', String(limit))
  return `/api/v1/items?${p}`
}

const base = '/api/v1/items'
export const listItems = (q: ItemQuery, cursor?: string | null, limit?: number) => api.get<Page<Item>>(itemsPath(q, cursor, limit))
export const getItem = (id: string) => api.get<Item>(`${base}/${id}`)
export const createItem = (fields: Record<string, unknown>) => api.post<Item>(base, { source: 'manual', ...fields })
/** Partial update; fields touched by a person leave `ai_fields`. */
export const patchItem = (id: string, fields: Record<string, unknown>) => api.patch<Item>(`${base}/${id}`, fields)
export const deleteItem = (id: string) => api.del<Item>(`${base}/${id}`)
export const restoreItem = (id: string) => api.post<Item>(`${base}/${id}/restore`)
export const moveItem = (id: string, placeId: string | null) => api.post<Item>(`${base}/${id}/move`, { place_id: placeId })
export const confirmItem = (id: string) => api.post<Item>(`${base}/${id}/confirm`)
export const markSeen = (id: string) => api.post<Item>(`${base}/${id}/seen`)
/** At most 500 ids; answers with the changed items. */
export const bulkItems = (ids: string[], change: { place_id?: string | null; status?: string }) =>
  api.post<Page<Item>>(`${base}/bulk`, { ids, ...change })
