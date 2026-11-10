import { api } from '../api'
import type { QueueEntry } from './store'

/**
 * POST /api/v1/photos, multipart. Fields: `file`, `role`, `id` (the queue entry id, used by the
 * server as the photo link id so a retry is idempotent), and `item_id` (the client ULID; the
 * server creates the draft item on the first photo) plus optional `place_id` for item roles, or
 * `place_id` alone for `contents`.
 */
export function buildForm(e: QueueEntry): FormData {
  const form = new FormData()
  form.append('id', e.id)
  form.append('role', e.role)
  if (e.role === 'contents') {
    if (e.placeId) form.append('place_id', e.placeId)
  } else {
    form.append('item_id', e.groupId)
    if (e.placeId) form.append('place_id', e.placeId)
  }
  form.append('file', e.blob!, 'photo.jpg')
  return form
}

export async function uploadEntry(e: QueueEntry): Promise<void> {
  if (!e.blob) throw new Error('Photo data is missing.')
  await api.upload('/api/v1/photos', buildForm(e))
}
