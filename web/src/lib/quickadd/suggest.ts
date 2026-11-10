// J7.2: the sheet's `suggest` function, backed by POST /api/v1/ai/complete-rows.
// It never throws and never shows an error: without a working AI it returns
// nothing, so the sheet behaves as without AI.
import { ApiError, api } from '../api'
import { ai } from '../ai.svelte'
import { showToast } from '../ui/toasts.svelte'
import type { SuggestFn, SuggestResult, SuggestRow } from './sheet.svelte'

/** One row as the endpoint takes it. */
interface WireRow {
  id: string
  name: string
  values: Record<string, string>
}
interface WireAnswer {
  rows: { id: string; cells: Record<string, string>; confidence?: Record<string, string> }[]
}

export interface SuggestOptions {
  /** Sends one request; defaults to the API. */
  post?: (rows: WireRow[]) => Promise<WireAnswer>
  /** Is AI usable right now? Defaults to the text role's status. */
  working?: () => boolean
  /** Rows asked for within this many ms of each other go in one request. */
  batchMs?: number
  /** Failures in a row before asking stops for the session. */
  maxFailures?: number
  /** How long to leave the server alone after "too many requests". */
  rateLimitMs?: number
  /** The one quiet notice when asking stops after repeated failures. */
  notify?: (message: string) => void
}

const MAX_ROWS = 20

const defaultPost = (rows: WireRow[]) => api.post<WireAnswer>('/api/v1/ai/complete-rows', { rows })

export function createSuggest(opts: SuggestOptions = {}): SuggestFn {
  const post = opts.post ?? defaultPost
  const working = opts.working ?? (() => ai.status('text') === 'working')
  const batchMs = opts.batchMs ?? 120
  const maxFailures = opts.maxFailures ?? 3
  const rateLimitMs = opts.rateLimitMs ?? 30_000
  const notify = opts.notify ?? ((m: string) => showToast(m, 'info', 6000))

  let stopped = false // for the session
  let failures = 0
  let waitUntil = 0
  /** The newest name asked for per row: an older answer for a changed name is dropped. */
  const latest = new Map<string, string>()
  let queue: { row: WireRow; done: (r: SuggestResult | undefined) => void }[] = []
  let timer: ReturnType<typeof setTimeout> | undefined

  function fail(e: unknown) {
    if (e instanceof ApiError && e.status === 409 && (e.code === 'ai_not_set_up' || e.code === 'ai_paused')) {
      stopped = true // the settings say no AI: stay silent
      return
    }
    if (e instanceof ApiError && e.status === 429) {
      waitUntil = Date.now() + rateLimitMs
      return
    }
    if (++failures >= maxFailures) {
      stopped = true
      notify('AI suggestions stopped for this session. The sheet works as usual.')
    }
  }

  async function send(batch: typeof queue) {
    let answer: WireAnswer | undefined
    try {
      answer = await post(batch.map((b) => b.row))
      failures = 0
    } catch (e) {
      fail(e)
    }
    const byId = new Map((answer?.rows ?? []).map((r) => [r.id, r]))
    for (const { row, done } of batch) {
      const r = byId.get(row.id)
      // The name changed while this request ran: the answer is for text that is gone.
      if (!r || latest.get(row.id) !== row.name) done(undefined)
      else done({ id: row.id, cells: r.cells ?? {} })
    }
  }

  function flush() {
    timer = undefined
    // A row asked twice within the window counts once, with its newest name.
    const newest = new Map(queue.map((q, i) => [q.row.id, i]))
    const all = queue.filter((q, i) => {
      if (newest.get(q.row.id) === i) return true
      q.done(undefined)
      return false
    })
    queue = []
    for (let i = 0; i < all.length; i += MAX_ROWS) void send(all.slice(i, i + MAX_ROWS))
  }

  return async (rows: SuggestRow[]): Promise<SuggestResult[]> => {
    if (stopped || Date.now() < waitUntil || !working()) return []
    const pending = rows
      .filter((r) => r.name.trim())
      .map((r) => {
        const values: Record<string, string> = {}
        if (r.values.category) values.category = r.values.category // only a category the person picked is passed on
        const row: WireRow = { id: r.id, name: r.name.trim(), values }
        latest.set(row.id, row.name)
        return new Promise<SuggestResult | undefined>((done) => queue.push({ row, done }))
      })
    if (!pending.length) return []
    timer ??= setTimeout(flush, batchMs)
    const out = await Promise.all(pending)
    return out.filter((r): r is SuggestResult => !!r && Object.keys(r.cells).length > 0)
  }
}
