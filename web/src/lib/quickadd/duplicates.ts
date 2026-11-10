// J7.6: duplicate hints. "Same thing" and "Add to quantity" fold a freshly added item into one that exists.
import { fetchPhotoBlob, uploadItemPhoto } from '../inventory/photos'
import { deleteItem, getItem, markSeen, moveItem, patchItem } from '../inventory/items'
import { similarApi, type Match } from '../search/api'

export interface NewItem {
  id: string
  place: string | null
  quantity: number
  /** Photo hashes of the new item that move over on "Same thing". */
  photos?: string[]
}

/** Worth asking the server: brand and model together, or a name long enough to be specific. */
export const canLookup = (v: { brand?: string; model?: string; name?: string }) => !!((v.brand?.trim() && v.model?.trim()) || (v.name ?? '').trim().length >= 5)

export const lookup = (v: { brand?: string; model?: string; name?: string }, excludeId: string): Promise<Match[]> =>
  canLookup(v) ? similarApi({ brand: v.brand ?? '', model: v.model ?? '', name: v.name ?? '' }, [excludeId]).catch(() => []) : Promise.resolve([])

/**
 * `same`: the new photos go to the existing item, it moves to the new place and "last seen" refreshes.
 * `add`: the quantity is added to the existing item. Either way the new item is deleted at the end.
 */
export async function mergeInto(existingId: string, n: NewItem, kind: 'same' | 'add'): Promise<void> {
  if (kind === 'add') {
    const ex = await getItem(existingId)
    await patchItem(existingId, { quantity: ex.quantity + n.quantity })
  } else {
    for (const sha of n.photos ?? []) await uploadItemPhoto(existingId, await fetchPhotoBlob(sha))
    if (n.place) await moveItem(existingId, n.place)
    await markSeen(existingId)
  }
  await deleteItem(n.id)
}
