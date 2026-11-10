// The "packing now" box of the signed-in person (GET /me → packing_box, PUT /me/packing-box).
// One shared instance: Moving, Capture and Quick add all read it.
import { api } from '../api'
import type { PlaceShort } from '../inventory/types'
import type { BoxStatus, GoesTo } from './boxes'

export interface PackingBox {
  id: string
  name: string
  box_number: number
  box_status: BoxStatus | null
  goes_to: GoesTo | null
}

/** The packing box as a place, for screens that file new things into it. */
export const boxPlace = (b: PackingBox): PlaceShort => ({ id: b.id, kind: 'box', name: b.name, path: b.name, home_id: null, box_number: b.box_number, color: null })

const V1 = '/api/v1'

class Packing {
  box = $state.raw<PackingBox | null>(null)
  loaded = $state(false)

  async load() {
    const me = await api.get<{ packing_box?: PackingBox | null }>(`${V1}/me`)
    this.box = me.packing_box ?? null
    this.loaded = true
  }

  /** Makes box `number` (or `null` for none) the one being packed. An unknown number is a 422. */
  async set(by: { number: number } | { box_id: string } | null) {
    const r = await api.put<{ packing_box: PackingBox | null }>(`${V1}/me/packing-box`, by)
    this.box = r.packing_box ?? null
  }

  /** Marks the box packed and stops packing it. */
  async close() {
    if (!this.box) return
    await api.post(`${V1}/boxes/${encodeURIComponent(this.box.id)}/close`)
    this.box = null
  }
}

export const packing = new Packing()
