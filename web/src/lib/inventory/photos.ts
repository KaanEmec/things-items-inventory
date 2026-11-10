import { api } from '../api'

export const MAX_SIDE = 2048

export const photoUrl = (sha256: string, size: 'orig' | 'screen' | 'thumb' = 'thumb') => `/api/v1/photos/${sha256}/${size}`

/** Size after scaling so the longest side is at most `max`. Never scales up. */
export function fitWithin(width: number, height: number, max = MAX_SIDE): { width: number; height: number } {
  const longest = Math.max(width, height)
  if (longest <= max) return { width, height }
  const k = max / longest
  return { width: Math.max(1, Math.round(width * k)), height: Math.max(1, Math.round(height * k)) }
}

/**
 * Re-encodes a picked file as JPEG with the longest side at most 2048 px.
 * The browser decodes it, so HEIC works wherever the browser can show it.
 */
export async function toJpeg(file: Blob, max = MAX_SIDE, quality = 0.86): Promise<Blob> {
  let bmp: ImageBitmap
  try {
    bmp = await createImageBitmap(file, { imageOrientation: 'from-image' })
  } catch {
    throw new Error('This browser cannot read that photo format. Try a JPEG or PNG.')
  }
  const { width, height } = fitWithin(bmp.width, bmp.height, max)
  const canvas = document.createElement('canvas')
  canvas.width = width
  canvas.height = height
  const ctx = canvas.getContext('2d')
  if (!ctx) throw new Error('Could not prepare the photo.')
  ctx.fillStyle = '#fff' // PNG transparency becomes white, not black
  ctx.fillRect(0, 0, width, height)
  ctx.drawImage(bmp, 0, 0, width, height)
  bmp.close()
  const blob = await new Promise<Blob | null>((res) => canvas.toBlob(res, 'image/jpeg', quality))
  if (!blob) throw new Error('Could not prepare the photo.')
  return blob
}

/** Uploads a photo and links it to the item (role `item` by default). */
export async function uploadItemPhoto(itemId: string, blob: Blob, role: 'item' | 'label' | 'receipt' = 'item') {
  const form = new FormData()
  form.append('item_id', itemId)
  form.append('role', role)
  form.append('file', blob, 'photo.jpg')
  return api.upload<{ photo: unknown; link: { id: string } }>('/api/v1/photos', form)
}

/** Moves a link to position `sort` (0 = first = main photo). */
export const moveLink = (linkId: string, sort: number) => api.patch(`/api/v1/photo-links/${linkId}`, { sort })
export const removeLink = (linkId: string) => api.del(`/api/v1/photo-links/${linkId}`)

/** Links an already uploaded photo again (undo of a delete). It goes to the end of the gallery. */
export const relink = (photoId: string, itemId: string, role: string) =>
  api.post('/api/v1/photo-links', { photo_id: photoId, item_id: itemId, role })

/** The original file of a stored photo. */
export async function fetchPhotoBlob(sha256: string): Promise<Blob> {
  const r = await fetch(photoUrl(sha256, 'orig'), { credentials: 'same-origin' })
  if (!r.ok) throw new Error('Could not read the photo.')
  return r.blob()
}
