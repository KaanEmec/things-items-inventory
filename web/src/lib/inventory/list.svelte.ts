import { explain } from '../errors'
import type { Page } from '../api'
import { listItems, type ItemQuery } from './items'
import type { Item } from './types'

/** The items list: cursor-paged, shared by the list pane and the detail panel. */
export class ItemList {
  items = $state.raw<Item[]>([])
  next = $state<string | null>(null)
  loading = $state(false)
  loaded = $state(false)
  error = $state('')
  #run = 0

  /** Starts over with a new query. A late answer to an older query is ignored. */
  async reset(q: ItemQuery) {
    const run = ++this.#run
    this.query = q
    this.items = []
    this.next = null
    this.loaded = false
    this.error = ''
    await this.#fetch(run, null)
  }
  query: ItemQuery = {}

  async more() {
    if (this.loading || !this.next) return
    await this.#fetch(this.#run, this.next)
  }

  async #fetch(run: number, cursor: string | null) {
    this.loading = true
    try {
      const page = await listItems(this.query, cursor)
      if (run !== this.#run) return
      this.items = cursor ? [...this.items, ...page.data] : page.data
      this.next = page.next_cursor
      this.error = ''
    } catch (e) {
      if (run !== this.#run) return
      this.error = explain(e).message
      this.next = null
    } finally {
      if (run === this.#run) {
        this.loading = false
        this.loaded = true
      }
    }
  }

  /**
   * Reloads what is already shown (same query, same number of rows) and swaps it in at once, so a
   * live update keeps the scroll position and never empties the list. Ignored while a load runs.
   */
  async refresh() {
    if (this.loading || !this.loaded) return
    const run = this.#run
    const want = Math.max(this.items.length, 1)
    try {
      let got: Item[] = []
      let cursor: string | null = null
      let more = true
      while (more && got.length < want) {
        const page: Page<Item> = await listItems(this.query, cursor)
        if (run !== this.#run) return
        got = [...got, ...page.data]
        cursor = page.next_cursor
        more = !!cursor
      }
      if (run !== this.#run || this.loading) return
      this.items = got
      this.next = cursor
    } catch {
      // keep what is shown; the next change tries again
    }
  }

  find = (id: string) => this.items.find((i) => i.id === id)

  /** Replaces changed items in place (an item that moved in the sort order stays where it is until the next load). */
  replace(...changed: Item[]) {
    if (!changed.length) return
    const by = new Map(changed.map((i) => [i.id, i]))
    this.items = this.items.map((i) => by.get(i.id) ?? i)
  }
  prepend(item: Item) {
    this.items = [item, ...this.items.filter((i) => i.id !== item.id)]
  }
  remove(id: string): number {
    const at = this.items.findIndex((i) => i.id === id)
    if (at >= 0) this.items = this.items.filter((i) => i.id !== id)
    return at
  }
  insert(item: Item, at: number) {
    const rest = this.items.filter((i) => i.id !== item.id)
    rest.splice(Math.min(Math.max(at, 0), rest.length), 0, item)
    this.items = rest
  }
}
