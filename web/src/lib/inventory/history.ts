// Plain sentences for history events. The server sends a machine-readable `summary`; this phrases it.
import { humanize, valueLabel } from './categories'

export interface HistoryActor {
  kind: 'user' | 'ai' | 'token'
  user_id: string | null
  name: string
  initials: string
  token_name: string | null
}
export interface HistoryEntry {
  id: string
  at: string
  kind: string
  actor: HistoryActor
  item_id: string | null
  item_name: string | null
  place_id: string | null
  place_name: string | null
  summary: Record<string, any>
  data?: unknown
}

const list = (xs: string[]) => (xs.length < 2 ? (xs[0] ?? '') : `${xs.slice(0, -1).join(', ')} and ${xs[xs.length - 1]}`)
const fieldName = (f: string) => f.replace(/^attributes[._]/, '').replace(/[._]/g, ' ')
/** "3rd drawer · left closet · bedroom" -> "3rd drawer" */
const placeName = (path: unknown) => (typeof path === 'string' && path ? path.split(' · ')[0] : '')

function fields(s: Record<string, any>): string {
  const all = (Array.isArray(s.fields) ? s.fields : []).map((f: string) => fieldName(String(f)))
  if (all.length === 0) return 'some details'
  return all.length > 3 ? `${list(all.slice(0, 3))} and ${all.length - 3} more` : list(all)
}

/** Who did it, for the sentence: "You", a first name, or "AI". */
export function actorLabel(a: HistoryActor, meId?: string | null): string {
  if (a.kind === 'ai') return 'AI'
  if (meId && a.user_id === meId) return 'You'
  return a.name.trim().split(/\s+/)[0] || 'Someone'
}

/** "E moved it to Box 12". Keeps the verb part separate so the actor can be bold in the UI. */
export function verb(e: HistoryEntry): string {
  const s = e.summary ?? {}
  const what = s.subject === 'lines' ? 'them' : 'it'
  switch (e.kind) {
    case 'created':
      return typeof s.count === 'number' && s.count > 1 ? `added ${s.count} items` : `added ${what}`
    case 'edited':
      return e.actor.kind === 'ai' ? `filled ${fields(s)}` : `changed ${fields(s)}`
    case 'moved': {
      const to = placeName(s.to)
      return to ? `moved ${what} to ${to}` : `took ${what} out of its place`
    }
    case 'status_changed':
      return s.to ? `set the status to ${valueLabel(String(s.to)).toLowerCase()}` : 'changed the status'
    case 'photo_added':
      return 'added a photo'
    case 'confirmed':
      return 'confirmed the details'
    case 'seen':
      return `saw ${what} here`
    case 'packed':
      return `packed ${what}`
    case 'opened':
      return 'opened the box'
    case 'arrived':
      return 'marked the box as arrived'
    case 'unpacked':
      return `unpacked ${what}`
    case 'deleted':
      return `deleted ${what}`
    case 'restored':
      return `restored ${what}`
    case 'settings_changed':
      return 'changed the settings'
    default:
      return humanize(e.kind).toLowerCase()
  }
}

export function sentence(e: HistoryEntry, meId?: string | null): string {
  const base = `${actorLabel(e.actor, meId)} ${verb(e)}`
  return e.actor.kind === 'token' && e.actor.token_name ? `${base} · via ${e.actor.token_name}` : base
}
