import { api } from '../api'
import type { Attribute, Catalog, Category } from './types'

let cached: Promise<Catalog> | undefined
/** The attribute sets, statuses and conditions. Fetched once per page load. */
export function getCatalog(): Promise<Catalog> {
  cached ??= api.get<Catalog>('/api/v1/categories').catch((e) => {
    cached = undefined
    throw e
  })
  return cached
}

/** `storage_gb` -> `Storage gb`; `usb_c` -> `Usb c`. Used for keys and enum values. */
export function humanize(key: string): string {
  const s = key.replace(/_/g, ' ')
  return s.charAt(0).toUpperCase() + s.slice(1)
}

const labels: Record<string, string> = { to_sell: 'To sell / donate', in_use: 'In use', like_new: 'Like new', usb_c: 'USB-C', micro_usb: 'Micro USB', wifi: 'Wi-Fi', hdmi: 'HDMI', ic: 'IC', spring_autumn: 'Spring / autumn' }
export const valueLabel = (v: string) => labels[v] ?? humanize(v)

export const categoryLabel = (key: string) => (key === 'other' ? 'Other' : humanize(key))

function fits(a: Attribute, v: unknown): unknown {
  switch (a.type) {
    case 'text':
      return typeof v === 'string' && v !== '' ? v : undefined
    case 'int':
      return typeof v === 'number' && Number.isInteger(v) ? v : typeof v === 'string' && /^-?\d+$/.test(v) ? v : undefined
    case 'number':
      return typeof v === 'number' || (typeof v === 'string' && v.trim() !== '' && Number.isFinite(Number(v))) ? v : undefined
    case 'bool':
      return typeof v === 'boolean' ? v : undefined
    case 'enum':
      return typeof v === 'string' && a.values?.includes(v) ? v : undefined
    case 'multi': {
      if (!Array.isArray(v)) return undefined
      const keep = v.filter((x) => typeof x === 'string' && a.values?.includes(x))
      return keep.length ? keep : undefined
    }
  }
}

/**
 * Attributes to keep when the category changes: those the new category also
 * has, with a value that is still valid there (same key, compatible type, enum
 * value still allowed). Everything else is dropped from the form.
 */
export function carryAttributes(attrs: Record<string, unknown>, to: Category | undefined): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const a of to?.attributes ?? []) {
    if (!(a.key in attrs)) continue
    const v = fits(a, attrs[a.key])
    if (v !== undefined) out[a.key] = v
  }
  return out
}
