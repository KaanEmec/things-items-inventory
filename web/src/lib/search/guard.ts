// One guard for every global and list shortcut.
import { ignoreShortcut, type KeyLike } from '../inventory/keys'

/** True when a plain-key shortcut (C, J, K, X ...) may fire for this event. */
export function canFireShortcut(e: KeyLike, dialogOpen: boolean): boolean {
  return !ignoreShortcut(e, dialogOpen)
}

export const dialogIsOpen = () => typeof document !== 'undefined' && !!document.querySelector('dialog[open]')

export const isPaletteKey = (e: { key: string; metaKey?: boolean; ctrlKey?: boolean; altKey?: boolean; shiftKey?: boolean }) =>
  (e.metaKey || e.ctrlKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === 'k'
