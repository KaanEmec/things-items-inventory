export interface ToastItem {
  id: number
  message: string
  kind: 'info' | 'success' | 'error'
  /** Optional button, e.g. Undo. */
  action?: { label: string; run: () => void }
}

export const toasts = $state<ToastItem[]>([])
let next = 1

export function showToast(message: string, kind: ToastItem['kind'] = 'info', ms = 4000, action?: ToastItem['action']) {
  const id = next++
  toasts.push({ id, message, kind, action })
  setTimeout(() => dismissToast(id), action ? Math.max(ms, 7000) : ms)
}

export function dismissToast(id: number) {
  const i = toasts.findIndex((t) => t.id === id)
  if (i >= 0) toasts.splice(i, 1)
}
