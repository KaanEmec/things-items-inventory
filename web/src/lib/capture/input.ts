import { toJpeg } from '../inventory/photos'

/** Re-encodes picked files as JPEG (longest side 2048 px; also turns HEIC into JPEG). Failures are collected, not thrown. */
export async function prepare(files: Iterable<File>): Promise<{ blobs: Blob[]; errors: string[] }> {
  const blobs: Blob[] = []
  const errors: string[] = []
  for (const f of files) {
    if (f.type && !f.type.startsWith('image/')) continue
    try {
      blobs.push(await toJpeg(f))
    } catch (e) {
      errors.push(e instanceof Error ? e.message : 'Could not prepare a photo.')
    }
  }
  return { blobs, errors }
}
