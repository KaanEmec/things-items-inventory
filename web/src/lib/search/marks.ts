// Highlight ranges from the search API: [start, end) in UTF-16 code units, which is what String.slice uses.
export type Mark = [number, number]
export interface Segment {
  text: string
  mark: boolean
}

/** Splits `text` into plain and marked pieces. Bad, overlapping or out-of-range marks are clamped or dropped. */
export function segments(text: string, marks: readonly Mark[] | null | undefined): Segment[] {
  if (!text) return []
  const ranges = (marks ?? [])
    .map(([a, b]) => [Math.max(0, Math.floor(a)), Math.min(text.length, Math.floor(b))] as Mark)
    .filter(([a, b]) => a < b)
    .sort((x, y) => x[0] - y[0])
  const out: Segment[] = []
  let at = 0
  for (const [a, b] of ranges) {
    const from = Math.max(a, at)
    if (b <= from) continue
    if (from > at) out.push({ text: text.slice(at, from), mark: false })
    out.push({ text: text.slice(from, b), mark: true })
    at = b
  }
  if (at < text.length) out.push({ text: text.slice(at), mark: false })
  return out
}
