import { api, type Page } from '../api'
import type { ContentsLine, Home, Item, PlaceShort, TreePlace } from './types'

export const listHomes = () => api.get<Page<Home>>('/api/v1/homes')
export const getTree = (homeId: string) => api.get<{ home: Home; places: TreePlace[] }>(`/api/v1/homes/${homeId}/tree`)

export interface TreeRow {
  place: TreePlace
  depth: number
  hasChildren: boolean
}

/** Flat places (parent_id links) to depth-first rows in `sort` order. Orphans become roots. */
export function buildRows(places: TreePlace[]): TreeRow[] {
  const ids = new Set(places.map((p) => p.id))
  const kids = new Map<string | null, TreePlace[]>()
  for (const p of places) {
    const key = p.parent_id && ids.has(p.parent_id) ? p.parent_id : null
    if (!kids.has(key)) kids.set(key, [])
    kids.get(key)!.push(p)
  }
  for (const list of kids.values()) list.sort((a, b) => a.sort - b.sort || a.name.localeCompare(b.name))
  const out: TreeRow[] = []
  const walk = (key: string | null, depth: number) => {
    for (const p of kids.get(key) ?? []) {
      out.push({ place: p, depth, hasChildren: kids.has(p.id) })
      walk(p.id, depth + 1)
    }
  }
  walk(null, 0)
  return out
}

/** Ids of the ancestors of a place, so the tree can open to it. */
export function ancestorIds(places: TreePlace[], id: string | null | undefined): Set<string> {
  const byId = new Map(places.map((p) => [p.id, p]))
  const out = new Set<string>()
  let cur = id ? byId.get(id) : undefined
  while (cur?.parent_id && !out.has(cur.parent_id)) {
    out.add(cur.parent_id)
    cur = byId.get(cur.parent_id)
  }
  return out
}

const fold = (s: string) => s.normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase()
/** Places whose name or path contains every word typed. */
export function searchPlaces(places: TreePlace[], q: string): TreePlace[] {
  const words = fold(q).split(/\s+/).filter(Boolean)
  if (!words.length) return []
  return places.filter((p) => {
    const hay = fold(p.path || p.name)
    return words.every((w) => hay.includes(w))
  })
}

// Recently used places, per household, in this browser (the server has no endpoint for it yet).
const key = (household: string) => `recent-places:${household}`
export function recentPlaces(household: string): PlaceShort[] {
  try {
    const v = JSON.parse(localStorage.getItem(key(household)) ?? '[]')
    return Array.isArray(v) ? v.slice(0, 8) : []
  } catch {
    return []
  }
}
export function rememberPlace(household: string, p: PlaceShort) {
  try {
    const next = [p, ...recentPlaces(household).filter((x) => x.id !== p.id)].slice(0, 8)
    localStorage.setItem(key(household), JSON.stringify(next))
  } catch {
    // storage unavailable: recents are a convenience only
  }
}

// ---- Places screen: writes, lines, photo (J3.5) ----

const placesBase = '/api/v1/places'
export type PlaceKind = 'floor' | 'room' | 'furniture' | 'spot'
export const getPlace = (id: string) => api.get<TreePlace & { children: TreePlace[] }>(`${placesBase}/${id}`)
export const createPlace = (body: { home_id?: string; parent_id?: string; kind: PlaceKind; name: string; sort?: number }) =>
  api.post<TreePlace>(placesBase, body)
/** Partial update. `parent_id: null` moves to the top of the home; `sort` is the position among siblings. */
export const patchPlace = (id: string, body: { name?: string; parent_id?: string | null; home_id?: string; sort?: number; goes_to_id?: string | null; note?: string | null }) =>
  api.patch<TreePlace>(`${placesBase}/${id}`, body)
/** Answers 409 `not_empty` (fields: places, items, lines as strings) unless the place is empty or `moveTo` is given. */
export const deletePlace = (id: string, moveTo?: string) =>
  api.del<TreePlace>(`${placesBase}/${id}${moveTo ? `?move_to=${encodeURIComponent(moveTo)}` : ''}`)
export const restorePlace = (id: string) => api.post<TreePlace>(`${placesBase}/${id}/restore`)

export const listLines = (placeId: string) => api.get<Page<ContentsLine>>(`${placesBase}/${placeId}/lines`)
export const createLine = (placeId: string, name: string, quantity = 1) =>
  api.post<Page<ContentsLine>>('/api/v1/lines', { place_id: placeId, lines: [{ name, quantity }] })
export const patchLine = (id: string, body: { name?: string; quantity?: number }) => api.patch<ContentsLine>(`/api/v1/lines/${id}`, body)
export const deleteLine = (id: string) => api.del<ContentsLine>(`/api/v1/lines/${id}`)
export const restoreLine = (id: string) => api.post<ContentsLine>(`/api/v1/lines/${id}/restore`)
export const promoteLine = (id: string) => api.post<Item>(`/api/v1/lines/${id}/promote`)

// The server's place JSON has `photo: null` for now, so the photo of a place is remembered in this browser
// (sha for display, link id so a replacement can remove the old link).
interface PlacePhoto {
  sha256: string
  link: string
}
const photoKey = (placeId: string) => `place-photo:${placeId}`
export function cachedPlacePhoto(placeId: string): PlacePhoto | null {
  try {
    const v = JSON.parse(localStorage.getItem(photoKey(placeId)) ?? 'null')
    return v && typeof v.sha256 === 'string' ? v : null
  } catch {
    return null
  }
}
/** Uploads an already downscaled JPEG as the photo of a place, replacing the one before. */
export async function uploadPlacePhoto(placeId: string, blob: Blob): Promise<PlacePhoto> {
  const form = new FormData()
  form.append('place_id', placeId)
  form.append('role', 'place')
  form.append('file', blob, 'photo.jpg')
  const res = await api.upload<{ photo: { sha256: string }; link: { id: string } }>('/api/v1/photos', form)
  const old = cachedPlacePhoto(placeId)
  if (old && old.link !== res.link.id) await api.del(`/api/v1/photo-links/${old.link}`).catch(() => {})
  const next = { sha256: res.photo.sha256, link: res.link.id }
  try {
    localStorage.setItem(photoKey(placeId), JSON.stringify(next))
  } catch {
    // storage unavailable: the photo still shows until the page reloads
  }
  return next
}
