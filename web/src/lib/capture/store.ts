// Persistence for the upload queue. IndexedDB in the browser; an in-memory
// copy when IndexedDB is unavailable (private mode) or in tests.

export type Role = 'item' | 'label' | 'receipt' | 'contents'
export type EntryState = 'waiting' | 'uploading' | 'uploaded' | 'failed'

export interface QueueEntry {
  id: string
  /** Client-generated item id (ULID); all photos of one thing share it. Contents photos use it only for grouping. */
  groupId: string
  role: Role
  /** The place for the new item, or the container for contents photos. */
  placeId: string | null
  /** The JPEG; null once uploaded (the blob is dropped from storage). */
  blob: Blob | null
  state: EntryState
  attempts: number
  error: string | null
  /** Creation order. */
  seq: number
  /** Earliest time (ms) the next try may start; 0 = now. */
  nextAt: number
  /** A typed quick-add line (J7.4): the create body for POST /items; `blob` is null. Sent like a photo, idempotent on `groupId`. */
  line?: Record<string, unknown>
}

export interface QueueStore {
  readonly persistent: boolean
  load(): Promise<QueueEntry[]>
  put(entry: QueueEntry): Promise<void>
  delete(id: string): Promise<void>
}

export class MemoryStore implements QueueStore {
  readonly persistent = false
  rows = new Map<string, QueueEntry>()
  async load() {
    return [...this.rows.values()].map((e) => ({ ...e }))
  }
  async put(e: QueueEntry) {
    this.rows.set(e.id, { ...e })
  }
  async delete(id: string) {
    this.rows.delete(id)
  }
}

const DB_NAME = 'inventory-capture'
const OS = 'entries'

function wrap<T>(req: IDBRequest<T>): Promise<T> {
  return new Promise((res, rej) => {
    req.onsuccess = () => res(req.result)
    req.onerror = () => rej(req.error)
  })
}

export class IdbStore implements QueueStore {
  readonly persistent = true
  private constructor(private db: IDBDatabase) {}

  /** Opens the database; rejects when IndexedDB is missing or blocked. */
  static open(factory: IDBFactory = indexedDB): Promise<IdbStore> {
    return new Promise((res, rej) => {
      let req: IDBOpenDBRequest
      try {
        req = factory.open(DB_NAME, 1)
      } catch (e) {
        return rej(e)
      }
      req.onupgradeneeded = () => req.result.createObjectStore(OS, { keyPath: 'id' })
      req.onsuccess = () => res(new IdbStore(req.result))
      req.onerror = () => rej(req.error)
      req.onblocked = () => rej(new Error('IndexedDB is blocked'))
    })
  }

  private os(mode: IDBTransactionMode) {
    return this.db.transaction(OS, mode).objectStore(OS)
  }
  async load() {
    return (await wrap(this.os('readonly').getAll())) as QueueEntry[]
  }
  async put(e: QueueEntry) {
    await wrap(this.os('readwrite').put(e))
  }
  async delete(id: string) {
    await wrap(this.os('readwrite').delete(id))
  }
}

/** IndexedDB when it works (a write test catches Safari private-mode quirks), else memory. */
export async function openStore(): Promise<{ store: QueueStore; fellBack: boolean }> {
  try {
    if (typeof indexedDB === 'undefined') throw new Error('no indexedDB')
    const s = await IdbStore.open()
    await s.put({ id: '__probe', groupId: '', role: 'item', placeId: null, blob: null, state: 'uploaded', attempts: 0, error: null, seq: 0, nextAt: 0 })
    await s.delete('__probe')
    return { store: s, fellBack: false }
  } catch {
    return { store: new MemoryStore(), fellBack: true }
  }
}
