// Recent searches and opened items, per browser. Storage may be missing or throw, so every access is guarded.
export interface RecentItem {
  id: string
  title: string
  path?: string
}
interface Store {
  getItem(k: string): string | null
  setItem(k: string, v: string): void
}

const defaultStore = (): Store | null => {
  try {
    return globalThis.localStorage ?? null
  } catch {
    return null
  }
}

function read<T>(store: Store | null, key: string): T[] {
  try {
    const v = JSON.parse(store?.getItem(key) ?? '[]')
    return Array.isArray(v) ? v : []
  } catch {
    return []
  }
}
function write(store: Store | null, key: string, v: unknown) {
  try {
    store?.setItem(key, JSON.stringify(v))
  } catch {
    /* private mode or full: recents are a convenience */
  }
}

const SEARCHES = 'inventory.recent-searches'
const OPENED = 'inventory.recent-items'

export function recentSearches(store = defaultStore()): string[] {
  return read<unknown>(store, SEARCHES).filter((x): x is string => typeof x === 'string')
}
export function rememberSearch(q: string, store = defaultStore(), max = 6) {
  const t = q.trim()
  if (t.length < 2) return
  write(store, SEARCHES, [t, ...recentSearches(store).filter((x) => x !== t)].slice(0, max))
}
export function recentItems(store = defaultStore()): RecentItem[] {
  return read<RecentItem>(store, OPENED).filter((x) => x && typeof x.id === 'string' && typeof x.title === 'string')
}
export function rememberItem(it: RecentItem, store = defaultStore(), max = 6) {
  write(store, OPENED, [it, ...recentItems(store).filter((x) => x.id !== it.id)].slice(0, max))
}
