// Upload queue for captured photos. Entries live in IndexedDB until the server has them;
// one uploader (2 at a time) runs while the app is open, retries with backoff, and resumes
// on app start and when the browser comes back online. The server call is idempotent on the
// ids; the in-flight guard here keeps the same entry from being sent twice.
import { ApiError } from '../api'
import { showToast } from '../ui/toasts.svelte'
import { openStore, type QueueEntry, type QueueStore, type Role } from './store'
import { ulid } from './ulid'
import { uploadEntry } from './upload'

export type { QueueEntry, Role, EntryState } from './store'

export const MAX_ATTEMPTS = 6

/** 1 s, 2 s, 4 s ... capped at 30 s. */
export const backoffMs = (attempts: number) => Math.min(30_000, 1000 * 2 ** Math.max(0, attempts - 1))

export interface QueueOptions {
  store?: QueueStore
  upload?: (e: QueueEntry) => Promise<void>
  isOnline?: () => boolean
  now?: () => number
  concurrency?: number
  maxAttempts?: number
  /** Called once when IndexedDB is unavailable and the queue keeps photos in memory only. */
  onFallback?: () => void
}

export interface NewPhoto {
  groupId: string
  role: Role
  placeId: string | null
  blob: Blob
}

/** True when trying again later can help. */
function retryable(e: unknown): boolean {
  if (e instanceof ApiError) return e.status === 0 || e.status === 408 || e.status === 429 || e.status >= 500
  return true
}

export class CaptureQueue {
  entries = $state.raw<QueueEntry[]>([])
  ready = $state(false)
  /** True when photos are kept in memory only (IndexedDB unavailable): they are lost if the app closes. */
  memoryOnly = $state(false)
  online = $state(true)

  private store: QueueStore | undefined
  private opts: QueueOptions
  private inflight = new Map<string, string>() // entry id -> group id
  private timer: ReturnType<typeof setTimeout> | undefined
  private seq = 0
  private starting: Promise<void> | undefined

  constructor(opts: QueueOptions = {}) {
    this.opts = opts
    this.store = opts.store
    this.online = this.isOnline()
  }

  /** Photos still to send: waiting or uploading. */
  get pending() {
    return this.entries.filter((e) => e.state === 'waiting' || e.state === 'uploading').length
  }
  get failed() {
    return this.entries.filter((e) => e.state === 'failed').length
  }

  private isOnline() {
    return this.opts.isOnline ? this.opts.isOnline() : typeof navigator === 'undefined' || navigator.onLine !== false
  }
  private now() {
    return this.opts.now ? this.opts.now() : Date.now()
  }

  /** Loads stored entries and starts uploading. Safe to call more than once. */
  start(): Promise<void> {
    return (this.starting ??= this.init())
  }

  private async init() {
    if (!this.store) {
      const r = await openStore()
      this.store = r.store
      if (r.fellBack) this.opts.onFallback?.()
    }
    this.memoryOnly = !this.store.persistent
    const stored = await this.store.load()
    const have = new Set(this.entries.map((e) => e.id))
    const loaded: QueueEntry[] = []
    for (const e of stored) {
      if (have.has(e.id)) continue
      if (e.state === 'uploaded') {
        await this.store.delete(e.id) // finished earlier; never keep or resend it
        continue
      }
      // Left `uploading` by a closed app: send again (the server ignores a repeat of the same id).
      loaded.push(e.state === 'uploading' ? { ...e, state: 'waiting', nextAt: 0 } : e)
    }
    this.entries = [...loaded, ...this.entries].sort((a, b) => a.seq - b.seq)
    this.seq = Math.max(this.seq, ...this.entries.map((e) => e.seq)) + 1
    this.ready = true
    this.pump()
  }

  /** Listens for the browser going online or offline. Returns the cleanup. */
  attach(): () => void {
    const on = () => this.setOnline(true)
    const off = () => this.setOnline(false)
    addEventListener('online', on)
    addEventListener('offline', off)
    return () => {
      removeEventListener('online', on)
      removeEventListener('offline', off)
    }
  }

  setOnline(v: boolean) {
    this.online = v
    if (v) {
      // Back online: waiting entries held for backoff may go now.
      this.entries = this.entries.map((e) => (e.state === 'waiting' ? { ...e, nextAt: 0 } : e))
      this.pump()
    }
  }

  async add(p: NewPhoto): Promise<QueueEntry> {
    const e: QueueEntry = {
      id: ulid(),
      groupId: p.groupId,
      role: p.role,
      placeId: p.placeId,
      blob: p.blob,
      state: 'waiting',
      attempts: 0,
      error: null,
      seq: this.seq++,
      nextAt: 0,
    }
    this.entries = [...this.entries, e]
    if (this.store) {
      try {
        await this.store.put(e)
      } catch (err) {
        this.entries = this.entries.filter((x) => x.id !== e.id)
        throw err
      }
    }
    this.pump()
    return e
  }

  /** Keeps a typed line (the create body of an item) until it can be sent, like a photo. */
  async addLine(groupId: string, line: Record<string, unknown>): Promise<QueueEntry> {
    const e: QueueEntry = { id: ulid(), groupId, role: 'item', placeId: null, blob: null, line, state: 'waiting', attempts: 0, error: null, seq: this.seq++, nextAt: 0 }
    this.entries = [...this.entries, e]
    await this.store?.put(e)
    this.pump()
    return e
  }

  private lineOf(groupId: string) {
    return this.entries.find((e) => e.line && e.groupId === groupId && e.state !== 'uploaded')
  }

  /** Changes a queued line that has not left yet. False when there is none (it was sent: patch the item instead). */
  async updateLine(groupId: string, fields: Record<string, unknown>): Promise<boolean> {
    const e = this.lineOf(groupId)
    if (!e || e.state === 'uploading' || this.inflight.has(e.id)) return false
    const line = { ...e.line, ...fields }
    for (const k of ['attributes', 'purchase'])
      if (fields[k] && typeof fields[k] === 'object') line[k] = { ...(e.line?.[k] as object), ...(fields[k] as object) }
    await this.update(e.id, { line })
    return true
  }

  /** Drops a queued line that has not left yet. */
  async removeLine(groupId: string): Promise<boolean> {
    const e = this.lineOf(groupId)
    return e ? this.remove(e.id) : false
  }

  /** Removes a photo that has not been sent. Returns false when it is uploading or uploaded. */
  async remove(id: string): Promise<boolean> {
    const e = this.get(id)
    if (!e || e.state === 'uploading' || e.state === 'uploaded' || this.inflight.has(id)) return false
    this.entries = this.entries.filter((x) => x.id !== id)
    await this.store?.delete(id)
    return true
  }

  /** Changes the role of a photo that has not been sent. */
  async setRole(id: string, role: Role): Promise<void> {
    const e = this.get(id)
    if (!e || e.state === 'uploading' || e.state === 'uploaded') return
    await this.update(id, { role })
  }

  /** Tries a failed (or backed-off) photo again now. A photo already in flight is left alone. */
  async retry(id: string): Promise<void> {
    const e = this.get(id)
    if (!e || this.inflight.has(id) || e.state === 'uploading' || e.state === 'uploaded') return
    await this.update(id, { state: 'waiting', attempts: 0, error: null, nextAt: 0 })
    this.pump()
  }

  async retryAll(): Promise<void> {
    for (const e of this.entries) if (e.state === 'failed') await this.retry(e.id)
  }

  /** Forgets finished photos so the list stays short. */
  clearUploaded() {
    this.entries = this.entries.filter((e) => e.state !== 'uploaded')
  }

  get(id: string) {
    return this.entries.find((e) => e.id === id)
  }

  private async update(id: string, patch: Partial<QueueEntry>, persist = true) {
    let next: QueueEntry | undefined
    this.entries = this.entries.map((e) => (e.id === id ? (next = { ...e, ...patch }) : e))
    if (next && persist) await this.store?.put(next)
  }

  private pump() {
    if (!this.ready) return
    clearTimeout(this.timer)
    this.timer = undefined
    if (!this.isOnlineNow()) return
    const max = this.opts.concurrency ?? 2
    const now = this.now()
    const blocked = new Set<string>()
    let soonest = Infinity
    for (const e of this.entries) {
      if (this.inflight.size >= max) break
      if (e.state !== 'waiting' || this.inflight.has(e.id)) continue
      const busy = [...this.inflight.values()].includes(e.groupId)
      // One photo per item at a time, in order, so the draft item is created once.
      if (busy || blocked.has(e.groupId)) continue
      if (e.nextAt > now) {
        blocked.add(e.groupId)
        soonest = Math.min(soonest, e.nextAt)
        continue
      }
      void this.run(e)
    }
    if (soonest < Infinity) this.timer = setTimeout(() => this.pump(), Math.max(0, soonest - this.now()))
  }

  private isOnlineNow() {
    this.online = this.isOnline()
    return this.online
  }

  private async run(e: QueueEntry) {
    if (this.inflight.has(e.id)) return // never the same entry twice
    this.inflight.set(e.id, e.groupId)
    const attempts = e.attempts + 1
    await this.update(e.id, { state: 'uploading', attempts, error: null }, false)
    try {
      await (this.opts.upload ?? uploadEntry)(e)
      await this.update(e.id, { state: 'uploaded', blob: null, error: null }, false)
      await this.store?.delete(e.id)
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Upload failed.'
      const offline = err instanceof ApiError && err.status === 0 && !this.isOnline()
      if (offline) {
        // No connection: wait for the online event without using up a try.
        await this.update(e.id, { state: 'waiting', attempts: attempts - 1, error: message, nextAt: 0 })
      } else if (retryable(err) && attempts < (this.opts.maxAttempts ?? MAX_ATTEMPTS)) {
        await this.update(e.id, { state: 'waiting', error: message, nextAt: this.now() + backoffMs(attempts) })
      } else {
        await this.update(e.id, { state: 'failed', error: message })
      }
    } finally {
      this.inflight.delete(e.id)
      this.pump()
    }
  }
}

export const captureQueue = new CaptureQueue({
  onFallback: () =>
    showToast('This browser cannot keep photos on the device. Keep the app open until they are uploaded, or they are lost.', 'info', 9000),
})
