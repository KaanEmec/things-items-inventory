import { api } from '../api'
import type { Mark } from './marks'

export interface Match {
  kind: 'item' | 'line' | 'place' | 'box'
  id: string
  title: string
  title_marks: Mark[]
  snippet: string
  snippet_marks: Mark[]
  path: string
  box_number?: number
  /** Similar only. */
  why?: 'brand_model' | 'name'
}
export interface SearchResponse {
  q: string
  top: Match | null
  items: Match[]
  lines: Match[]
  places: Match[]
  boxes: Match[]
}

export function searchPath(q: string, limit = 6): string {
  return `/api/v1/search?${new URLSearchParams({ q, limit: String(limit) })}`
}
export const searchApi = (q: string, _signal?: AbortSignal) => api.get<SearchResponse>(searchPath(q))

export interface SimilarQuery {
  brand: string
  model: string
  name: string
}
/** Items that look like the one being added (J7.6). Generic names match nothing. */
export const similarApi = (q: SimilarQuery, exclude: string[] = []) =>
  api.get<{ items: Match[] }>(`/api/v1/search/similar?${new URLSearchParams({ ...q, exclude: exclude.join(',') })}`).then((r) => r.items)
