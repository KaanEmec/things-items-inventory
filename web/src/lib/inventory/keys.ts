// Keyboard guard for list shortcuts: they must never fire while someone types.

interface Targetish {
  tagName?: string
  isContentEditable?: boolean
  getAttribute?: (n: string) => string | null
  closest?: (sel: string) => unknown
}

const textInputTypes = new Set(['text', 'search', 'email', 'url', 'tel', 'password', 'number', 'date', 'time', 'datetime-local', 'month', 'week'])

/** True for fields where typing a letter means text, not a shortcut. */
export function isTypingTarget(t: EventTarget | null): boolean {
  const el = t as Targetish | null
  if (!el || !el.tagName) return false
  const tag = el.tagName.toUpperCase()
  if (tag === 'TEXTAREA' || tag === 'SELECT') return true
  if (tag === 'INPUT') {
    const type = (el.getAttribute?.('type') ?? 'text').toLowerCase()
    return textInputTypes.has(type)
  }
  if (el.isContentEditable) return true
  return el.getAttribute?.('role') === 'textbox'
}

export interface KeyLike {
  key: string
  target: EventTarget | null
  metaKey?: boolean
  ctrlKey?: boolean
  altKey?: boolean
  defaultPrevented?: boolean
}

/** True when a list shortcut must not react to this key press. */
export function ignoreShortcut(e: KeyLike, dialogOpen = false): boolean {
  return !!(e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey || dialogOpen || isTypingTarget(e.target))
}

/** Action for a list key, or undefined. */
export function listAction(key: string): 'next' | 'prev' | 'open' | 'select' | 'clear' | undefined {
  switch (key) {
    case 'j':
    case 'J':
      return 'next'
    case 'k':
    case 'K':
      return 'prev'
    case 'Enter':
      return 'open'
    case 'x':
    case 'X':
      return 'select'
    case 'Escape':
      return 'clear'
  }
}
