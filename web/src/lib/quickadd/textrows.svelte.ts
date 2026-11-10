// J7.3: "Describe or paste". Text goes to POST /api/v1/ai/text-to-items (long text in parts); the rows come back as
// suggestions and nothing is saved until "Add all".
import { api } from '../api'
import { explain } from '../errors'
import type { Sheet } from './sheet.svelte'

export const MAX_CHARS = 12000

/** Cut text at line breaks into parts of at most `max` characters; a longer single line is cut hard. */
export function splitText(text: string, max = MAX_CHARS): string[] {
  const parts: string[] = []
  let cur = ''
  const push = () => {
    if (cur.trim()) parts.push(cur.trim())
    cur = ''
  }
  for (let line of text.replace(/\r\n?/g, '\n').split('\n')) {
    while (line.length > max) {
      push()
      parts.push(line.slice(0, max))
      line = line.slice(max)
    }
    if (cur.length + line.length + 1 > max) push()
    cur += (cur ? '\n' : '') + line
  }
  push()
  return parts
}

export interface TextRow {
  source: string
  values: Record<string, unknown>
  confidence: Record<string, string>
  place_id: string | null
}
interface Answer {
  ai: boolean
  rows: TextRow[]
}
export type PostText = (text: string) => Promise<Answer>
const defaultPost: PostText = (text) => api.post<Answer>('/api/v1/ai/text-to-items', { text })

/** A suggested row as the sheet takes it. */
export function toSheetRow(r: TextRow, ai: boolean): Parameters<Sheet['addRows']>[0][number] {
  const values: Record<string, string> = {}
  const purchase: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(r.values)) {
    if (v == null || v === '') continue
    if (k === 'purchase_date') purchase.date = v
    else if (k === 'price_minor') purchase.price_minor = v
    else if (k === 'currency') purchase.currency = v
    else if (k === 'seller') purchase.seller = v
    else values[k.startsWith('attributes.') ? `attr.${k.slice(11)}` : k] = String(v)
  }
  return { values, ai, place: r.place_id, extra: Object.keys(purchase).length ? { purchase } : undefined }
}

export class Describe {
  text = $state('')
  rows = $state.raw<TextRow[]>([])
  ai = $state(true)
  busy = $state(false)
  done = $state(0)
  total = $state(0)
  error = $state('')
  #post: PostText

  constructor(post: PostText = defaultPost) {
    this.#post = post
  }

  async run() {
    const parts = splitText(this.text)
    if (!parts.length || this.busy) return
    this.busy = true
    this.error = ''
    this.done = 0
    this.total = parts.length
    const rows: TextRow[] = []
    try {
      for (const p of parts) {
        const a = await this.#post(p)
        this.ai = a.ai
        rows.push(...a.rows)
        this.done++
      }
      this.rows = rows
      this.text = ''
    } catch (e) {
      // Keep what the finished parts gave; the person can add it or try again.
      this.rows = rows
      this.error = explain(e).message
    } finally {
      this.busy = false
    }
  }

  remove(i: number) {
    this.rows = this.rows.filter((_, j) => j !== i)
  }
  clear() {
    this.rows = []
    this.error = ''
  }

  /** Add all: the rows become saved drafts in the sheet. */
  addAll(sheet: Sheet) {
    sheet.addRows(this.rows.map((r) => toSheetRow(r, this.ai)))
    this.rows = []
  }
}
