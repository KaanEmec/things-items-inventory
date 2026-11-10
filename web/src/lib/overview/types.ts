// GET /api/v1/overview (internal/inventory/overview.go).
export interface Counts {
  all: number
  drafts: number
  packed: number
  to_sell: number
  in_warranty: number
}

export interface Actor {
  kind: 'user' | 'ai' | 'token'
  user_id: string | null
  name: string
  initials: string
  token_name: string | null
}

export interface RecentEntry {
  id: string
  at: string
  kind: string
  actor: Actor
  item_id: string | null
  item_name: string | null
  place_id: string | null
  place_name: string | null
  summary: Record<string, unknown>
}

export type NeedsKind = 'drafts' | 'warranty_ending' | 'unseen' | 'failed_jobs' | 'not_found'

export interface NeedsRow {
  id: string
  item_id: string | null
  name: string
  category: string | null
  place_path: string | null
  /** Warranty end (a date), last seen or last change, or when the job failed. */
  at: string | null
  /** The error of a failed job. */
  detail: string | null
}

export interface NeedsGroup {
  kind: NeedsKind
  count: number
  rows: NeedsRow[]
}

export interface MovingRoom {
  room_id: string | null
  name: string | null
  color: string | null
  total: number
  packed: number
}

export interface Moving {
  total: number
  by_status: Record<string, number>
  rooms: MovingRoom[]
}

export interface Overview {
  counts: Counts
  places: number
  recent: RecentEntry[]
  needs_look: NeedsGroup[]
  moving: Moving | null
}
