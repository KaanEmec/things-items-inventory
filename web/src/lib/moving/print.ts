// Pure parts of label printing: ranges, sheet maths, the print URL, room colours.
// The DOM helpers at the bottom are guarded so the rest runs under node.

export type Paper = 'a4' | 'letter'
export const PAPERS: Record<Paper, { label: string; w: number; h: number }> = {
  a4: { label: 'A4', w: 210, h: 297 },
  letter: { label: 'US Letter', w: 215.9, h: 279.4 },
}

/** Sheet margin and the gap between cut cells, in mm. */
export const MARGIN = 10
export const GAP = 5
/** Box label: full usable width, 80 mm tall (three per sheet). Place label: 90 x 40 mm. */
export const BOX_LABEL = { w: 190, h: 80 }
export const PLACE_LABEL = { w: 90, h: 40 }

export interface SheetLayout {
  cols: number
  rows: number
  perSheet: number
  /** Cell width in mm (box labels fill the width). */
  w: number
  h: number
}

export function layout(paper: Paper, kind: 'box' | 'place'): SheetLayout {
  const { w: pw, h: ph } = PAPERS[paper]
  const uw = pw - 2 * MARGIN
  const uh = ph - 2 * MARGIN
  const cell = kind === 'box' ? BOX_LABEL : PLACE_LABEL
  const cols = Math.max(1, Math.floor((uw + GAP) / (cell.w + GAP) + 1e-9))
  const rows = Math.max(1, Math.floor((uh + GAP) / (cell.h + GAP) + 1e-9))
  return { cols, rows, perSheet: cols * rows, w: cell.w, h: cell.h }
}

export const COPIES_MIN = 1
export const COPIES_MAX = 4
export const clampCopies = (n: number) => (Number.isFinite(n) ? Math.min(COPIES_MAX, Math.max(COPIES_MIN, Math.round(n))) : 3)

export const labelCount = (boxes: number, copies: number) => boxes * clampCopies(copies)

/** Sheets for `boxes` box labels and `places` place labels (they never share a sheet). */
export function sheetsNeeded(boxes: number, copies: number, places: number, paper: Paper): number {
  const b = layout(paper, 'box').perSheet
  const p = layout(paper, 'place').perSheet
  return Math.ceil(labelCount(boxes, copies) / b) + Math.ceil(places / p)
}

export function paginate<T>(items: T[], perSheet: number): T[][] {
  const out: T[][] = []
  for (let i = 0; i < items.length; i += perSheet) out.push(items.slice(i, i + perSheet))
  return out
}

// ---- ranges: "3-7, 12" -------------------------------------------------------

export interface ParsedRange {
  numbers: number[]
  /** Pieces that could not be read, as typed. */
  invalid: string[]
}

export function parseRange(text: string, limit = 100000): ParsedRange {
  const set = new Set<number>()
  const invalid: string[] = []
  for (const raw of text.split(/[,;\s]+/)) {
    const part = raw.trim()
    if (!part) continue
    const m = /^(\d+)(?:\s*[-–]\s*(\d+))?$/.exec(part)
    if (!m) {
      invalid.push(part)
      continue
    }
    const a = Number(m[1])
    const b = m[2] === undefined ? a : Number(m[2])
    if (a < 1 || b < a || b - a >= limit) {
      invalid.push(part)
      continue
    }
    for (let n = a; n <= b; n++) set.add(n)
  }
  return { numbers: [...set].sort((x, y) => x - y), invalid }
}

/** [3,4,5,6,7,12] -> "3-7,12". */
export function formatRange(numbers: number[]): string {
  const ns = [...new Set(numbers)].sort((a, b) => a - b)
  const out: string[] = []
  for (let i = 0; i < ns.length; ) {
    let j = i
    while (j + 1 < ns.length && ns[j + 1] === ns[j] + 1) j++
    out.push(j > i ? `${ns[i]}-${ns[j]}` : `${ns[i]}`)
    i = j + 1
  }
  return out.join(',')
}

// ---- print URL ---------------------------------------------------------------

export interface PrintRequest {
  /** Box numbers (not ids). */
  boxes: number[]
  /** Place ids. */
  places: string[]
  copies: number
  paper: Paper
  mono: boolean
  qr: boolean
}

export const defaultRequest = (): PrintRequest => ({ boxes: [], places: [], copies: 3, paper: 'a4', mono: false, qr: false })

export function buildPrintUrl(r: PrintRequest): string {
  const q = new URLSearchParams()
  if (r.boxes.length) q.set('boxes', formatRange(r.boxes))
  if (r.places.length) q.set('places', r.places.join(','))
  q.set('copies', String(clampCopies(r.copies)))
  q.set('paper', r.paper)
  if (r.mono) q.set('mono', '1')
  if (r.qr) q.set('qr', '1')
  return `/print/labels?${q}`
}

export function parsePrintUrl(search: string): PrintRequest {
  const q = new URLSearchParams(search)
  const d = defaultRequest()
  const copies = q.get('copies')
  const paper = q.get('paper')
  const flag = (k: string) => q.get(k) === '1' || q.get(k) === 'true'
  return {
    boxes: parseRange(q.get('boxes') ?? '').numbers,
    places: (q.get('places') ?? '').split(',').map((s) => s.trim()).filter(Boolean),
    copies: copies === null ? d.copies : clampCopies(Number(copies)),
    paper: paper === 'letter' ? 'letter' : 'a4',
    mono: flag('mono'),
    qr: flag('qr'),
  }
}

/** Link a QR code points to: the box or place page. Works for signed-in household members only. */
export const placeLink = (origin: string, id: string) => `${origin}/places?place=${encodeURIComponent(id)}`

// ---- room colours ------------------------------------------------------------

export interface Tint {
  bg: string
  fg: string
}
// The server stores a colour name per room.
const NAMED: Record<string, Tint> = {
  red: { bg: '#DC4B3E', fg: '#FFFFFF' },
  orange: { bg: '#F59E0B', fg: '#1A1A19' },
  yellow: { bg: '#FACC15', fg: '#1A1A19' },
  green: { bg: '#22A06B', fg: '#FFFFFF' },
  teal: { bg: '#0E8F9C', fg: '#FFFFFF' },
  blue: { bg: '#3B6FE0', fg: '#FFFFFF' },
  purple: { bg: '#8B5CF6', fg: '#FFFFFF' },
  pink: { bg: '#E0509A', fg: '#FFFFFF' },
  brown: { bg: '#8B5E3C', fg: '#FFFFFF' },
  grey: { bg: '#6B7280', fg: '#FFFFFF' },
}
export const NEUTRAL: Tint = { bg: '#E7E6E0', fg: '#1A1A19' }

export function roomTint(color: string | null | undefined): Tint {
  if (!color) return NEUTRAL
  const named = NAMED[color.toLowerCase()]
  if (named) return named
  const m = /^#([0-9a-f]{6})$/i.exec(color)
  if (!m) return NEUTRAL
  const n = parseInt(m[1], 16)
  const lum = (0.299 * (n >> 16) + 0.587 * ((n >> 8) & 255) + 0.114 * (n & 255)) / 255
  return { bg: color, fg: lum > 0.6 ? '#1A1A19' : '#FFFFFF' }
}

export const PATTERNS = ['stripes', 'dots', 'check', 'cross', 'hlines'] as const
export type Pattern = (typeof PATTERNS)[number]
/** The same room always gets the same black-and-white pattern. */
export function patternFor(roomName: string): Pattern {
  let h = 0
  for (const c of roomName.toLowerCase()) h = (h * 31 + c.charCodeAt(0)) >>> 0
  return PATTERNS[h % PATTERNS.length]
}

export const HANDLING_ICON = { heavy: 'weight', fragile: 'glass', soft: 'cloud', this_side_up: 'arrow-up' } as const
export const HANDLING_LABEL = { heavy: 'Heavy', fragile: 'Fragile', soft: 'Soft', this_side_up: 'This side up' } as const

// ---- printed-at memory (per browser; the server has no write field yet) -----------

const printedKey = (household: string) => `labels-printed:${household}`
export function printedMap(household: string): Record<string, string> {
  try {
    return JSON.parse(localStorage.getItem(printedKey(household)) ?? '{}') ?? {}
  } catch {
    return {}
  }
}
export function rememberPrinted(household: string, boxIds: string[], when = new Date().toISOString()) {
  try {
    const m = printedMap(household)
    for (const id of boxIds) m[id] = when
    localStorage.setItem(printedKey(household), JSON.stringify(m))
  } catch {
    // private mode: nothing to remember
  }
}

// ---- DOM: print only the sheet ---------------------------------------------------

/**
 * Svelte action: while the browser prints, move the node to <body> and hide every
 * other child of body, so the app shell (fixed height, overflow) cannot clip pages.
 */
export function printSheet(node: HTMLElement) {
  let parent: Node | null = null
  let next: Node | null = null
  const before = () => {
    parent = node.parentNode
    next = node.nextSibling
    node.setAttribute('data-print-sheet', '')
    document.body.appendChild(node)
  }
  const after = () => {
    if (parent && node.parentNode === document.body) parent.insertBefore(node, next)
    parent = null
  }
  window.addEventListener('beforeprint', before)
  window.addEventListener('afterprint', after)
  return {
    destroy() {
      window.removeEventListener('beforeprint', before)
      window.removeEventListener('afterprint', after)
      after()
    },
  }
}
