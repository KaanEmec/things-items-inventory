// What the person is doing in Capture right now: the mode, the place new things go to, and the
// current group of photos (one thing). "Next item" closes a group and opens the next one.
import type { PlaceShort } from '../inventory/types'
import type { CaptureQueue, QueueEntry, Role } from './queue.svelte'
import { ulid } from './ulid'

export type Mode = 'item' | 'contents'

/** Group ids for photos dropped together: one shared id, or one per photo. */
export function groupIds(count: number, split: boolean, make: () => string = ulid): string[] {
  if (!split) {
    const id = make()
    return Array.from({ length: count }, () => id)
  }
  return Array.from({ length: count }, () => make())
}

const placeKey = (household: string) => `capture.place.${household}`

export function loadPlace(household: string): PlaceShort | null {
  try {
    const raw = localStorage.getItem(placeKey(household))
    return raw ? (JSON.parse(raw) as PlaceShort) : null
  } catch {
    return null
  }
}
function savePlace(household: string, p: PlaceShort | null) {
  try {
    if (p) localStorage.setItem(placeKey(household), JSON.stringify(p))
    else localStorage.removeItem(placeKey(household))
  } catch {
    /* storage unavailable: the place just is not remembered */
  }
}

export class CaptureSession {
  mode = $state<Mode>('item')
  place = $state.raw<PlaceShort | null>(null)
  /** The open group: the item id (or, in Contents mode, a grouping id). */
  groupId = $state(ulid())
  /** Groups closed with "Next item" since Capture opened. */
  done = $state(0)

  constructor(
    private queue: CaptureQueue,
    private household = '',
    private makeId: () => string = ulid,
  ) {
    this.groupId = makeId()
    if (household) this.place = loadPlace(household)
  }

  /** Photos of the open group, oldest first. */
  get shots(): QueueEntry[] {
    return this.queue.entries.filter((e) => e.groupId === this.groupId)
  }
  shotsOf(role: Role) {
    return this.shots.filter((e) => e.role === role)
  }

  setPlace(p: PlaceShort | null) {
    this.place = p
    savePlace(this.household, p)
  }

  setMode(m: Mode) {
    if (m === this.mode) return
    this.mode = m
    if (this.shots.length) this.groupId = this.makeId()
  }

  /** Queues photos into the open group. Contents photos need a place (the container). */
  async take(wanted: Role | string, blobs: Blob[]): Promise<QueueEntry[]> {
    // An unknown or empty role (a picker without a tag) falls back to what the mode takes.
    const known: string[] = ['item', 'label', 'receipt', 'contents']
    const role = (known.includes(wanted) ? wanted : this.mode === 'contents' ? 'contents' : 'item') as Role
    if (role === 'contents' && !this.place) throw new Error('Choose the container first.')
    const out: QueueEntry[] = []
    for (const blob of blobs) out.push(await this.queue.add({ groupId: this.groupId, role, placeId: this.place?.id ?? null, blob }))
    return out
  }

  /** Closes the open group and starts the next. Does nothing (false) while the group has no photo. */
  nextItem(): boolean {
    if (!this.shots.length) return false
    this.groupId = this.makeId()
    this.done++
    return true
  }

  /** Desktop: photos dropped or pasted. One item, or one per photo. Returns the group ids used. */
  async addFiles(blobs: Blob[], split: boolean): Promise<string[]> {
    const ids = groupIds(blobs.length, split, this.makeId)
    for (let i = 0; i < blobs.length; i++) {
      await this.queue.add({ groupId: ids[i], role: this.mode === 'contents' ? 'contents' : 'item', placeId: this.place?.id ?? null, blob: blobs[i] })
    }
    return [...new Set(ids)]
  }
}
