// Draft review: reading an item's `ai_draft`, and the AI routes. Pure helpers, no UI.
import { api } from '../api'
import { confirmItem } from '../inventory/items'
import type { Item } from '../inventory/types'

export type Confidence = 'sure' | 'likely' | 'guess'
export interface Need {
  kind: 'label_photo' | 'choose' | 'receipt_photo'
  field?: string
  question: string
  options?: string[]
}
/** A product line of the receipt that is not this item (J6.5). */
export interface ReceiptLine {
  text: string
  name: string | null
  quantity: number | null
  price: number | null
}
export interface Draft {
  confidence: Record<string, Confidence>
  needs: Need[]
  error: string | null
  receipt_lines: ReceiptLine[]
}

export function readDraft(item: Pick<Item, 'ai_draft'>): Draft {
  const d = (item.ai_draft ?? {}) as Partial<Draft>
  return { confidence: d.confidence ?? {}, needs: d.needs ?? [], error: d.error ?? null, receipt_lines: d.receipt_lines ?? [] }
}

/** AI-filled and not yet touched by a person (editing removes a field from `ai_fields`). */
export const isAiField = (item: Pick<Item, 'ai_fields'>, path: string) => item.ai_fields.includes(path)
export const isGuess = (item: Item, path: string) => isAiField(item, path) && readDraft(item).confidence[path] === 'guess'

/** Every AI-filled field is sure and nothing is asked: safe to confirm in bulk. */
export function isSure(item: Item): boolean {
  const d = readDraft(item)
  return item.ai_state === 'ready' && d.needs.length === 0 && item.ai_fields.every((f) => d.confidence[f] === 'sure')
}

export type QueueState = 'none' | 'waiting' | 'reading' | 'ready' | 'failed'
export function queueState(item: Pick<Item, 'ai_state'>): QueueState {
  switch (item.ai_state) {
    case 'reading':
      return 'reading'
    case 'failed':
      return 'failed'
    case 'ready':
      return 'ready'
    case 'queued':
      return 'waiting'
    default:
      return 'none' // null: no AI for this draft
  }
}
export const stateText: Record<QueueState, string> = {
  none: 'No AI',
  waiting: 'Waiting',
  reading: 'AI reading',
  ready: 'Ready to review',
  failed: 'Failed',
}

/** Answering a `choose` question: the field becomes the person's (the server drops it from `ai_fields`). */
export function answerFields(need: Need, value: string): Record<string, unknown> {
  const f = need.field ?? ''
  return f.startsWith('attributes.') ? { attributes: { [f.slice('attributes.'.length)]: value } } : { [f]: value }
}

export const askAi = (id: string, hint?: string) => api.post<Item>(`/api/v1/items/${id}/ai`, hint?.trim() ? { hint: hint.trim() } : {})
export const readWaiting = () => api.post<unknown>('/api/v1/items/ai/read-waiting')
export const confirmDraft = confirmItem
export const confirmSure = () => api.post<unknown>('/api/v1/items/confirm-sure')

/** Queue a read of the item's newest label or receipt photo, or of a container's contents photos. */
export const readLabel = (id: string) => api.post<unknown>(`/api/v1/items/${id}/label/read`)
export const readReceipt = (id: string) => api.post<unknown>(`/api/v1/items/${id}/receipt/read`)
export const readContents = (placeId: string) => api.post<unknown>(`/api/v1/places/${placeId}/contents/read`)

/** Where the warranty end came from (`warranty.source`). */
export function warrantySourceText(source: string | null | undefined): string {
  if (source === 'stated') return 'Stated on the receipt'
  if (source === 'default') return 'Household default length'
  return ''
}
export const confirmLines = (placeId: string) => api.post<unknown>(`/api/v1/places/${placeId}/lines/confirm`)
export const promoteAllLines = (placeId: string) => api.post<unknown>(`/api/v1/places/${placeId}/lines/promote-all`)
