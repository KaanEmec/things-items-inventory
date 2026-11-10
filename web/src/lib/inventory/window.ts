// Simple fixed-row windowing for long lists.

/** Index range [start, end) to render for the scroll position, with `overscan` rows on both sides. */
export function windowRange(scrollTop: number, viewport: number, rowHeight: number, count: number, overscan = 6) {
  const first = Math.floor(Math.max(0, scrollTop) / rowHeight)
  const last = Math.ceil((Math.max(0, scrollTop) + viewport) / rowHeight)
  return { start: Math.max(0, first - overscan), end: Math.min(count, last + overscan) }
}

/** New scrollTop that brings row `index` into view, or the same one when it already is. */
export function scrollToRow(scrollTop: number, viewport: number, rowHeight: number, index: number): number {
  const top = index * rowHeight
  if (top < scrollTop) return top
  if (top + rowHeight > scrollTop + viewport) return top + rowHeight - viewport
  return scrollTop
}
