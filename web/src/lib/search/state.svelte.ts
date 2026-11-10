import type { IconName } from '../icons'
import { navigate } from '../router.svelte'
import { baseActions, matchActions, moveSelectedAction } from './actions'
import { searchApi, type Match, type SearchResponse } from './api'
import type { Mark } from './marks'
import { recentItems, recentSearches, rememberItem, rememberSearch } from './recents'
import { SearchRunner } from './runner'

export interface Row {
  key: string
  group: string
  icon: IconName
  label: string
  labelMarks?: Mark[]
  sub?: string
  subMarks?: Mark[]
  hint?: string
  run: () => void
}

const DEBOUNCE_MS = 120

/** Where a place, box or contents line opens: the Places screen at that place. */
export const placeTarget = (m: Match) => (m.kind === 'line' ? `/places?line=${encodeURIComponent(m.id)}` : `/places?place=${encodeURIComponent(m.id)}`)

class Palette {
  open = $state(false)
  query = $state('')
  cursor = $state(0)
  results = $state.raw<SearchResponse | null>(null)
  loading = $state(false)
  error = $state(false)
  /** The Items list registers its selection here so "Move selected to…" can appear. */
  selection = $state.raw<{ count: number; move: () => void } | null>(null)
  /** Bumped on each open so recents are read fresh. */
  #opened = $state(0)
  #timer: ReturnType<typeof setTimeout> | undefined
  #runner = new SearchRunner(searchApi)

  show() {
    this.#opened++
    this.cursor = 0
    this.open = true
  }
  hide() {
    this.open = false
    clearTimeout(this.#timer)
    this.#runner.cancel()
    this.loading = false
  }
  toggle() {
    this.open ? this.hide() : this.show()
  }
  /** Opens with text already typed (phone field, "search for X" links). */
  showWith(q: string) {
    this.query = q
    this.show()
    this.setQuery(q)
  }

  setQuery(q: string) {
    this.query = q
    this.cursor = 0
    clearTimeout(this.#timer)
    const t = q.trim()
    if (!t) {
      this.#runner.cancel()
      this.results = null
      this.loading = false
      this.error = false
      return
    }
    this.loading = true
    this.#timer = setTimeout(async () => {
      try {
        const res = await this.#runner.run(t)
        if (!res) return // superseded
        this.results = res
        this.error = false
        this.loading = false
      } catch {
        this.error = true
        this.loading = false
      }
    }, DEBOUNCE_MS)
  }

  #go(to: string, remember?: { id: string; title: string; path?: string }) {
    rememberSearch(this.query)
    if (remember) rememberItem(remember)
    this.hide()
    navigate(to)
  }

  rows = $derived.by<Row[]>(() => {
    this.#opened
    const q = this.query.trim()
    const out: Row[] = []
    if (!q) {
      for (const it of recentItems())
        out.push({ key: `ri:${it.id}`, group: 'Recently opened', icon: 'tag', label: it.title, sub: it.path, run: () => this.#go(`/items/${it.id}`, it) })
      for (const s of recentSearches())
        out.push({ key: `rs:${s}`, group: 'Recent searches', icon: 'clock', label: s, run: () => this.setQuery(s) })
    } else if (this.results) {
      const add = (list: Match[], group: string, icon: IconName) => {
        for (const m of list)
          out.push({
            key: `${m.kind}:${m.id}`,
            group,
            icon,
            label: m.kind === 'box' && m.box_number != null ? `Box ${m.box_number} · ${m.title}` : m.title,
            labelMarks: m.kind === 'box' && m.box_number != null ? undefined : m.title_marks,
            sub: m.kind === 'item' ? [m.snippet, m.path].filter(Boolean).join(' · ') : m.path || m.snippet,
            run: () => (m.kind === 'item' ? this.#go(`/items/${m.id}`, { id: m.id, title: m.title, path: m.path }) : this.#go(placeTarget(m))),
          })
      }
      add(this.results.items, 'Items', 'tag')
      add(this.results.places, 'Places', 'pin')
      add(this.results.boxes, 'Boxes', 'package')
      add(this.results.lines, 'In containers', 'package')
    }
    const actions = [...(this.selection?.count ? [moveSelectedAction(this.selection.count)] : []), ...baseActions]
    for (const a of matchActions(actions, q))
      out.push({
        key: `a:${a.id}`,
        group: 'Actions',
        icon: a.icon,
        label: a.label,
        hint: a.hint,
        run: () => {
          if (a.to) return this.#go(a.to)
          const move = this.selection?.move
          this.hide()
          move?.()
        },
      })
    return out
  })

  move(delta: number) {
    const n = this.rows.length
    if (n) this.cursor = (this.cursor + delta + n) % n
  }
  runCurrent() {
    this.rows[Math.min(this.cursor, this.rows.length - 1)]?.run()
  }
}

export const palette = new Palette()
