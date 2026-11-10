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
