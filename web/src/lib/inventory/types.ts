// Shapes of the JSON the server sends (checked against internal/inventory and internal/photos).

export interface PlaceShort {
  id: string
  kind: 'floor' | 'room' | 'furniture' | 'spot' | 'box'
  name: string
  /** Inside-out: `3rd drawer · left closet · bedroom`. */
  path: string
  home_id: string | null
  box_number: number | null
  color: string | null
}

/** One row of GET /homes/{id}/tree (flat, parent_id links, counts). */
export interface TreePlace extends PlaceShort {
  parent_id: string | null
  sort: number
  counts?: { items: number; items_below: number; lines: number }
  /** Destination room in the new home. */
  goes_to?: { id: string; name: string; color: string | null } | null
  note?: string | null
  daily_use?: boolean
  /** The server sends `null` today; place photos are looked up by the Places screen. */
  photo?: { sha256?: string } | null
}

/** A contents line (cheap or bulky things logged per container). */
export interface ContentsLine {
  id: string
  place_id: string
  name: string
  quantity: number
  category: string | null
  worth_item: boolean
  ai: boolean
  photo_id: string | null
  item_id?: string | null
  created_at: string
}

export interface Home {
  id: string
  name: string
  sort: number
  archived_at: string | null
}

export interface ItemPhoto {
  link_id: string
  photo_id: string
  sha256: string
  role: 'item' | 'label' | 'receipt' | 'contents' | 'place'
  sort: number
}

export interface Item {
  id: string
  name: string
  category: string
  brand: string | null
  model: string | null
  serial: string | null
  color: string | null
  quantity: number
  condition: string | null
  notes: string | null
  status: string
  attributes: Record<string, unknown>
  ai_fields: string[]
  ai_state: string | null
  ai_draft?: unknown
  draft: boolean
  source: string
  import_id: string | null
  place: PlaceShort | null
  purchase: { date: string | null; price_minor: number | null; currency: string | null; seller: string | null }
  warranty: { end: string | null; source: string | null }
  last_seen_at: string
  photos: ItemPhoto[]
  created_by: string | null
  updated_by: { id: string; name: string } | null
  created_at: string
  updated_at: string
}

export type AttrType = 'text' | 'int' | 'number' | 'bool' | 'enum' | 'multi'
export interface Attribute {
  key: string
  type: AttrType
  values?: string[]
  hint?: string
}
export interface Category {
  key: string
  attributes: Attribute[]
}
/** GET /categories (the spec says "sets, statuses, conditions"; the server also sends place kinds and room colours). */
export interface Catalog {
  categories: Category[]
  statuses: string[]
  conditions: string[]
}
