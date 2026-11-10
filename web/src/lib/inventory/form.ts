// Turns form text into the JSON the items API takes, and back.
import type { Attribute, Item } from './types'

export const blank = (s: string) => (s.trim() === '' ? null : s.trim())

/** "12,50" or "12.5" -> 1250 minor units. A value that is not a number is passed on as text so the server reports it. */
export function toMinor(s: string): number | string | null {
  const t = s.trim().replace(',', '.')
  if (t === '') return null
  const n = Number(t)
  return Number.isFinite(n) ? Math.round(n * 100) : t
}
export const fromMinor = (n: number | null | undefined) => (n == null ? '' : (n / 100).toFixed(2))

/** Text typed for an attribute to the value the API takes. */
export function attrValue(a: Attribute, v: unknown): unknown {
  if (v == null || v === '' || (Array.isArray(v) && !v.length)) return null
  if (a.type === 'int' || a.type === 'number') {
    const n = Number(String(v).replace(',', '.'))
    return String(v).trim() !== '' && Number.isFinite(n) ? n : String(v)
  }
  return v
}

/** Attribute values as form state: numbers become text, lists stay lists. */
export function attrsToForm(attrs: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(attrs ?? {})) out[k] = typeof v === 'number' ? String(v) : v
  return out
}

export const coreFields = (it: Item) => ({
  name: it.name,
  category: it.category,
  status: it.status,
  brand: it.brand ?? '',
  model: it.model ?? '',
  serial: it.serial ?? '',
  color: it.color ?? '',
  quantity: String(it.quantity),
  condition: it.condition ?? '',
  notes: it.notes ?? '',
  purchase_date: it.purchase.date?.slice(0, 10) ?? '',
  price: fromMinor(it.purchase.price_minor),
  seller: it.purchase.seller ?? '',
  warranty_end: it.warranty.end?.slice(0, 10) ?? '',
})
export type CoreFields = ReturnType<typeof coreFields>
