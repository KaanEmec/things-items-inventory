// J7.4: lines typed without a connection wait in the capture upload queue (J6.2) and are sent when it is back.
// The item id is the line's id, and creating an item twice with one id gives one item, so nothing appears twice.
import { ApiError } from '../api'
import type { CaptureQueue } from '../capture/queue.svelte'
import type { Item } from '../inventory/types'
import { itemsApi, type SheetApi } from './sheet.svelte'

const offline = (e: unknown) => (e instanceof ApiError && e.status === 0) || (typeof navigator !== 'undefined' && navigator.onLine === false)

export function offlineApi(q: Pick<CaptureQueue, 'addLine' | 'updateLine' | 'removeLine'>, base: SheetApi = itemsApi): SheetApi {
  return {
    create: async (f) => {
      try {
        return await base.create(f)
      } catch (e) {
        if (!offline(e)) throw e
        await q.addLine(String(f.id), f)
        return { id: f.id } as Item
      }
    },
    patch: async (id, f) => ((await q.updateLine(id, f)) ? ({ id } as Item) : base.patch(id, f)),
    remove: async (id) => ((await q.removeLine(id)) ? {} : base.remove(id)),
    restore: (id) => base.restore(id),
  }
}
