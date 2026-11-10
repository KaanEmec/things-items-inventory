import { ApiError } from '../api'
import { explain } from '../errors'
import {
  buildRows,
  createPlace,
  deletePlace,
  getTree,
  listHomes,
  patchPlace,
  restorePlace,
  type PlaceKind,
} from '../inventory/places'
import type { Home, Item, TreePlace } from '../inventory/types'
import { countsAfterItemMove, initialExpanded, reorderTarget, siblingsOf, toggled, visibleRows } from './logic'

/** The text to show for a failed call: the general message, else the first field message (the server puts "cannot be moved under itself" in `fields`). */
export function failure(e: unknown): string {
  const { message, fields } = explain(e)
  return message || Object.values(fields)[0] || 'Something went wrong. Try again.'
}

const homeKey = 'places-home'

/** The places of one home: tree rows, expand state, and the edits that change them. */
export class PlacesStore {
  homes = $state.raw<Home[]>([])
  homeId = $state('')
  places = $state.raw<TreePlace[]>([])
  expanded = $state.raw(new Set<string>())
  loaded = $state(false)
  error = $state('')
  #run = 0
  #opened = false

  byId = $derived(new Map(this.places.map((p) => [p.id, p])))
  rows = $derived(buildRows(this.places))
  visible = $derived(visibleRows(this.rows, this.expanded))
  roots = $derived(this.places.filter((p) => !p.parent_id || !this.byId.has(p.parent_id)))
  /** Only the starter rooms (or nothing) exist: no furniture, no spots. */
  onlyRooms = $derived(this.loaded && this.places.every((p) => !p.parent_id))
  home = $derived(this.homes.find((h) => h.id === this.homeId))

  children = (id: string | null) =>
    this.places.filter((p) => (p.parent_id ?? null) === id).sort((a, b) => a.sort - b.sort || a.name.localeCompare(b.name))

  async init(selectedId?: string | null) {
    try {
      this.homes = (await listHomes()).data
      let id = ''
      try {
        id = localStorage.getItem(homeKey) ?? ''
      } catch {
        // storage unavailable
      }
      this.homeId = this.homes.some((h) => h.id === id) ? id : (this.homes[0]?.id ?? '')
      if (!this.homeId) {
        this.loaded = true
        return
      }
      await this.load(selectedId)
    } catch (e) {
      this.error = failure(e)
      this.loaded = true
    }
  }

  async switchHome(id: string) {
    this.homeId = id
    this.#opened = false
    try {
      localStorage.setItem(homeKey, id)
    } catch {
      // storage unavailable
    }
    this.loaded = false
    await this.load()
  }

  /** (Re)loads the tree. A late answer to an older call is ignored. */
  async load(selectedId?: string | null) {
    const run = ++this.#run
    const home = this.homeId
    try {
      const t = await getTree(home)
      if (run !== this.#run) return
      this.places = t.places
      this.error = ''
      if (!this.#opened) {
        this.expanded = initialExpanded(t.places, selectedId)
        this.#opened = true
      }
    } catch (e) {
      if (run === this.#run) this.error = failure(e)
    } finally {
      if (run === this.#run) this.loaded = true
    }
  }

  /** Quietly brings the tree back in line with the server (after an edit or a failure). */
  refresh = () => this.load()

  /** Opens the way to a place; if it belongs to another home, switches to it. */
  reveal(id: string) {
    const p = this.byId.get(id)
    if (!p) return
    let open = this.expanded
    for (let cur = this.byId.get(p.parent_id ?? ''); cur; cur = this.byId.get(cur.parent_id ?? '')) open = toggled(open, cur.id, true)
    if (open !== this.expanded) this.expanded = open
  }

  toggle(id: string, open?: boolean) {
    this.expanded = toggled(this.expanded, id, open)
  }

  #patchLocal(id: string, change: Partial<TreePlace>) {
    this.places = this.places.map((p) => (p.id === id ? { ...p, ...change } : p))
  }

  /** Creates places one after the other; the ones that worked stay even if a later one fails. */
  async add(parentId: string | null, kind: PlaceKind, names: string[]): Promise<TreePlace[]> {
    const made: TreePlace[] = []
    try {
      for (const name of names) {
        const p = await createPlace({ kind, name, ...(parentId ? { parent_id: parentId } : { home_id: this.homeId }) })
        made.push(p)
        this.places = [...this.places, { ...p, counts: p.counts ?? { items: 0, items_below: 0, lines: 0 } }]
      }
    } finally {
      if (parentId && made.length) this.toggle(parentId, true)
    }
    return made
  }

  async rename(id: string, name: string) {
    const prev = this.byId.get(id)?.name
    this.#patchLocal(id, { name })
    try {
      await patchPlace(id, { name })
    } catch (e) {
      if (prev !== undefined) this.#patchLocal(id, { name: prev })
      throw e
    } finally {
      this.refresh() // paths of everything below changed
    }
  }

  async setGoesTo(id: string, goesTo: string | null) {
    await patchPlace(id, { goes_to_id: goesTo })
    await this.refresh()
  }

  /** Moves a place under `parentId` (null = top level) at position `sort`. Shown at once, undone when the server refuses. */
  async move(id: string, parentId: string | null, sort?: number) {
    const before = this.places
    this.#patchLocal(id, { parent_id: parentId, ...(sort === undefined ? {} : { sort }) })
    if (parentId) this.toggle(parentId, true)
    try {
      await patchPlace(id, { parent_id: parentId, ...(sort === undefined ? {} : { sort }), ...(parentId ? {} : { home_id: this.homeId }) })
    } catch (e) {
      this.places = before
      throw e
    } finally {
      this.refresh()
    }
  }

  /** One step up or down among the siblings. Returns false at the ends. */
  async reorder(id: string, step: -1 | 1): Promise<boolean> {
    const to = reorderTarget(this.places, id, step)
    if (to === null) return false
    const me = this.byId.get(id)!
    await this.move(id, me.parent_id, to)
    return true
  }

  /** Deletes a place. A 409 `not_empty` is thrown as it is so the caller can ask where the contents go. */
  async remove(id: string, moveTo?: string): Promise<TreePlace> {
    const out = await deletePlace(id, moveTo)
    this.places = this.places.filter((p) => p.id !== id)
    this.refresh()
    return out
  }

  async restore(id: string) {
    await restorePlace(id)
    await this.refresh()
  }

  /** Shows the new counts at once after items moved; the server's numbers follow. */
  itemsMoved(items: Pick<Item, 'place'>[], target: string) {
    this.places = countsAfterItemMove(this.places, items, target)
    this.refresh()
  }

  siblings = (id: string) => siblingsOf(this.places, id)
}

export const isNotEmpty = (e: unknown): e is ApiError => e instanceof ApiError && e.code === 'not_empty'
