export interface ToastItem {
  id: number
  message: string
  kind: 'info' | 'success' | 'error'
}

export const toasts = $state<ToastItem[]>([])
let next = 1

export function showToast(message: string, kind: ToastItem['kind'] = 'info', ms = 4000) {
  const id = next++
  toasts.push({ id, message, kind })
  setTimeout(() => {
    const i = toasts.findIndex((t) => t.id === id)
    if (i >= 0) toasts.splice(i, 1)
  }, ms)
}
