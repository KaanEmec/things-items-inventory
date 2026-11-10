// Live updates: one server-sent event stream for the signed-in session.
//
// Screens call `stream.on(type, fn)` (every message) or `stream.onBurst(types, fn)` (one call
// after a quiet moment, so a bulk change refreshes a screen once) and refetch what they show.
// Messages are hints, never data: a missed one only costs a stale screen until the next.
import { untrack } from 'svelte'
import { api, ApiError } from './api'
import { auth } from './auth.svelte'

export type StreamType = 'item' | 'place' | 'box' | 'job' | 'resync'
export interface StreamMessage {
  type: StreamType
  id: string
  /** Event kind (moved, edited, ...) or, for jobs, the job state. */
  kind: string
  job_kind?: string
  item_id?: string
  place_id?: string
}

/** The part of EventSource the store uses, so tests can fake it. */
export interface SourceLike {
  addEventListener(type: string, fn: (e: any) => void): void
  close(): void
}

export interface StreamOptions {
  open?: (url: string) => SourceLike
  /** Asks the server whether the session is still good after a dropped connection; a 401 must reject with ApiError. */
  probe?: () => Promise<unknown>
  /** Quiet time before a burst is delivered, and the longest a steady stream can hold it back. */
  burstMs?: number
  burstMaxMs?: number
  backoff?: (attempt: number) => number
}

const URL = '/api/v1/stream'

export function createStream(o: StreamOptions = {}) {
  const open = o.open ?? ((u) => new EventSource(u) as unknown as SourceLike)
  const probe = o.probe ?? (() => api.get('/api/v1/me'))
  const burstMs = o.burstMs ?? 300
  const burstMaxMs = o.burstMaxMs ?? 2000
  const backoff = o.backoff ?? ((n) => Math.min(30000, 1000 * 2 ** n))

  let connected = $state(false)
  let source: SourceLike | null = null
  let wanted = false
  let attempt = 0
  let everOpened = false
  let retry: ReturnType<typeof setTimeout> | undefined
  const subs = new Set<{ types: ReadonlySet<string>; fn: (m: StreamMessage) => void }>()

  function dispatch(m: StreamMessage) {
    for (const s of [...subs]) if (s.types.has(m.type)) s.fn(m)
  }

  function connect() {
    if (!wanted || source) return
    const src = open(URL)
    source = src
    src.addEventListener('open', () => {
      if (source !== src) return
      connected = true
      attempt = 0
      // Changes made while we were away are unknown: everything refetches once.
      if (everOpened) dispatch({ type: 'resync', id: '', kind: 'reconnected' })
      everOpened = true
    })
    for (const name of ['event', 'job']) {
      src.addEventListener(name, (e: MessageEvent) => {
        if (source !== src) return
        try {
          const m = JSON.parse(e.data) as StreamMessage
          if (m && typeof m.type === 'string') dispatch(m)
        } catch {
          // not ours
        }
      })
    }
    src.addEventListener('error', () => {
      if (source !== src) return
      // EventSource hides the status code, and would loop on a 401. Stop it, ask the API, retry later.
      src.close()
      source = null
      connected = false
      void probe().then(
        () => schedule(),
        (e) => {
          // A 401 has already sent the person to Login (the API client does that); stop here.
          if (!(e instanceof ApiError && e.status === 401)) schedule()
        },
      )
    })
  }

  function schedule() {
    if (!wanted || retry) return
    retry = setTimeout(() => {
      retry = undefined
      connect()
    }, backoff(attempt++))
  }

  function start() {
    if (wanted) return
    wanted = true
    attempt = 0
    connect()
  }

  function stop() {
    wanted = false
    everOpened = false
    clearTimeout(retry)
    retry = undefined
    source?.close()
    source = null
    connected = false
  }

  function on(type: StreamType | StreamType[], fn: (m: StreamMessage) => void): () => void {
    const s = { types: new Set(Array.isArray(type) ? type : [type]) as ReadonlySet<string>, fn }
    subs.add(s)
    return () => void subs.delete(s)
  }

  /** `fn` runs once per burst with the messages it caught. `match` narrows what counts. */
  function onBurst(
    type: StreamType | StreamType[],
    fn: (batch: StreamMessage[]) => void,
    match?: (m: StreamMessage) => boolean,
  ): () => void {
    let batch: StreamMessage[] = []
    let quiet: ReturnType<typeof setTimeout> | undefined
    let max: ReturnType<typeof setTimeout> | undefined
    const flush = () => {
      clearTimeout(quiet)
      clearTimeout(max)
      quiet = max = undefined
      const b = batch
      batch = []
      if (b.length) fn(b)
    }
    const off = on(type, (m) => {
      if (match && !match(m)) return
      batch.push(m)
      clearTimeout(quiet)
      quiet = setTimeout(flush, burstMs)
      max ??= setTimeout(flush, burstMaxMs)
    })
    return () => {
      off()
      clearTimeout(quiet)
      clearTimeout(max)
      batch = []
    }
  }

  return {
    get connected() { return connected },
    start,
    stop,
    on,
    onBurst,
  }
}

export const stream = createStream()

// Open after sign-in (and again when the active household changes), close on sign-out.
if (typeof EventSource !== 'undefined') {
  $effect.root(() => {
    $effect(() => {
      if (auth.status !== 'ready') return
      auth.householdId
      untrack(() => stream.start())
      return () => stream.stop()
    })
  })
}
