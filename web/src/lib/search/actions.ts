import type { IconName } from '../icons'

export interface Action {
  id: string
  label: string
  icon: IconName
  /** Route to open; actions without one run a handler. */
  to?: string
  hint?: string
  /** Extra words that match. */
  words?: string
}

export const baseActions: Action[] = [
  { id: 'new-item', label: 'New item', icon: 'plus', to: '/items?new=1', words: 'add create' },
  { id: 'capture', label: 'Capture', icon: 'camera', to: '/capture', hint: 'C', words: 'photo' },
  { id: 'quick-add', label: 'Quick add', icon: 'table', to: '/quick-add', words: 'spreadsheet bulk' },
  { id: 'go-overview', label: 'Go to Overview', icon: 'dashboard', to: '/', words: 'home' },
  { id: 'go-items', label: 'Go to Items', icon: 'tag', to: '/items' },
  { id: 'go-places', label: 'Go to Places', icon: 'pin', to: '/places', words: 'rooms boxes' },
  { id: 'go-moving', label: 'Go to Moving', icon: 'truck', to: '/moving', words: 'boxes' },
  { id: 'go-settings', label: 'Go to Settings', icon: 'sliders', to: '/settings' },
]

export const moveSelectedAction = (n: number): Action => ({
  id: 'move-selected',
  label: `Move selected to… (${n})`,
  icon: 'move',
  words: 'move selected items place',
})

/** Actions that match every word of the query; all of them when it is empty. */
export function matchActions(list: Action[], q: string): Action[] {
  const words = q.toLowerCase().split(/\s+/).filter(Boolean)
  if (!words.length) return list
  return list.filter((a) => {
    const hay = `${a.label} ${a.words ?? ''}`.toLowerCase()
    return words.every((w) => hay.includes(w))
  })
}
