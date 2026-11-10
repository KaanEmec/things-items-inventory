// The quick-add sheet as a plain model: rows, columns, cursor, edit state, undo and per-row save state.
// No DOM here; the grid component only renders it and forwards key and paste events.
import { ulid } from '../capture/ulid'
import { explain } from '../errors'
import { categoryLabel, humanize, valueLabel } from '../inventory/categories'
import { createItem, deleteItem, patchItem, restoreItem } from '../inventory/items'
import { searchPlaces } from '../inventory/places'
import type { Attribute, Catalog, Category, Item, PlaceShort } from '../inventory/types'

export type SaveState = 'new' | 'saving' | 'saved' | 'failed'
export type ColumnKind = 'text' | 'int' | 'number' | 'select' | 'multi' | 'place'

export interface Column {
  /** `name`, `quantity`, ..., or `attr.<key>` for a category attribute. */
  key: string
  label: string
  kind: ColumnKind
  options?: { value: string; label: string }[]
  attr?: Attribute
  mono?: boolean
  /** Width in px. */
  width: number
}

export interface Row {
  id: string
  /** Cell text by column key. For `place` it is the place id. */
  values: Record<string, string>
  state: SaveState
  error: string
  /** True once the server has the item. */
  created: boolean
  /** Column keys whose current value was accepted from a suggestion. */
  ai: string[]
  /** Ghost text by column key; only ever shown in empty cells. */
  suggestions: Record<string, string>
}

export interface SheetApi {
  create(fields: Record<string, unknown>): Promise<Item>
  patch(id: string, fields: Record<string, unknown>): Promise<Item>
  remove(id: string): Promise<unknown>
  restore(id: string): Promise<unknown>
}

export const itemsApi: SheetApi = {
  create: (f) => createItem(f),
  patch: (id, f) => patchItem(id, f),
  remove: (id) => deleteItem(id),
  restore: (id) => restoreItem(id),
}

/** What the sheet hands to `suggest`: the typed values of a row, by column key. */
export interface SuggestRow {
  id: string
  name: string
  values: Record<string, string>
}
/** One answer per row: ghost text by column key. Rows may be missing. */
export interface SuggestResult {
  id: string
  cells: Record<string, string>
}
/** J7.2 plugs the AI in here. Called with every row whose name cell was just left, batched per tick. */
export type SuggestFn = (rows: SuggestRow[]) => Promise<SuggestResult[]>

export interface SheetOptions {
  api?: SheetApi
  suggest?: SuggestFn
  /** Quiet time before a changed cell is sent. */
  debounceMs?: number
  /**
   * Send `ai_fields` (field paths of accepted cells) with create/patch.
   * Off: the server does not accept the field yet, it would answer 400.
   */
  sendAiFields?: boolean
  /** Called once per accepted suggestion cell, for the host (e.g. activity). */
  onAccept?: (rowId: string, key: string) => void
}

export interface KeyInput {
  key: string
  shiftKey?: boolean
  metaKey?: boolean
  ctrlKey?: boolean
  altKey?: boolean
}

export interface Edit {
  row: number
  col: number
  draft: string
  /** True when started by typing a character (replaces the cell) rather than F2/double-click. */
  typed: boolean
}

interface Cursor {
  row: number
  col: number
}

type Change = { rowId: string; key: string; before: string; after: string; beforeAi?: boolean }
type UndoEntry =
  | { kind: 'cells'; changes: Change[]; addedRows?: string[] }
  | { kind: 'delete'; row: Row; index: number }

/** A row is `new` until it has a name and its cursor has left it. */
const BASE_KEYS = ['name', 'quantity', 'category', 'brand', 'model', 'place', 'status']

export const parseClipboard = (text: string): string[][] => {
  const lines = text.replace(/\r\n?/g, '\n').split('\n')
  if (lines.length > 1 && lines[lines.length - 1] === '') lines.pop()
  return lines.map((l) => l.split('\t'))
}

const attrKey = (k: string) => (k.startsWith('attr.') ? k.slice(5) : '')
const fold = (s: string) => s.normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase().trim()

export class Sheet {
  catalog: Catalog
  api: SheetApi
  suggest: SuggestFn | undefined
  debounceMs: number
  sendAiFields: boolean
  onAccept: ((rowId: string, key: string) => void) | undefined

  category = $state('other')
  defaults = $state({ place: '', status: 'stored' })
  rows = $state<Row[]>([])
  cursor = $state<Cursor>({ row: 0, col: 0 })
  anchor = $state<Cursor>({ row: 0, col: 0 })
  editing = $state<Edit | null>(null)
  /** Places seen so far (picked, or loaded by the screen), by id. */
  places = $state.raw<Record<string, PlaceShort>>({})
  undoDepth = $state(0)

  columns = $derived(this.#columnsFor(this.category))
  added = $derived(this.rows.filter((r) => r.created).length)
  failed = $derived(this.rows.filter((r) => r.state === 'failed').length)
  saving = $derived(this.rows.filter((r) => r.state === 'saving').length)

  #undo: UndoEntry[] = []
  #rev = new Map<string, Record<string, number>>()
  #acked = new Map<string, Record<string, number>>()
  #queue = new Map<string, Promise<unknown>>()
  #timers = new Map<string, ReturnType<typeof setTimeout>>()
  #suggestedFor = new Map<string, string>()
  #suggestPending = new Set<string>()
  #suggestScheduled = false
  #typeahead = { text: '', at: 0 }

  constructor(catalog: Catalog, opts: SheetOptions = {}) {
    this.catalog = catalog
    this.api = opts.api ?? itemsApi
    this.suggest = opts.suggest
    this.debounceMs = opts.debounceMs ?? 400
    this.sendAiFields = opts.sendAiFields ?? false
    this.onAccept = opts.onAccept
    const keys = catalog.categories.map((c) => c.key)
    this.category = keys.includes('other') ? 'other' : (keys[0] ?? 'other')
    this.defaults.status = catalog.statuses.includes('stored') ? 'stored' : (catalog.statuses[0] ?? '')
    this.rows.push(this.#blankRow())
  }

  // ---- columns -------------------------------------------------------------

  #cat(key: string): Category | undefined {
    return this.catalog.categories.find((c) => c.key === key)
  }

  #columnsFor(category: string): Column[] {
    const cols: Column[] = [
      { key: 'name', label: 'Name', kind: 'text', width: 240 },
      { key: 'quantity', label: 'Qty', kind: 'int', mono: true, width: 64 },
      { key: 'category', label: 'Category', kind: 'select', options: this.catalog.categories.map((c) => ({ value: c.key, label: categoryLabel(c.key) })), width: 130 },
      { key: 'brand', label: 'Brand', kind: 'text', width: 130 },
      { key: 'model', label: 'Model', kind: 'text', mono: true, width: 140 },
      { key: 'place', label: 'Place', kind: 'place', width: 220 },
      { key: 'status', label: 'Status', kind: 'select', options: this.catalog.statuses.map((s) => ({ value: s, label: valueLabel(s) })), width: 130 },
    ]
    for (const a of this.#cat(category)?.attributes ?? []) {
      const base = { key: `attr.${a.key}`, label: humanize(a.key), attr: a, width: 130 }
      if (a.type === 'enum') cols.push({ ...base, kind: 'select', options: [{ value: '', label: '' }, ...(a.values ?? []).map((v) => ({ value: v, label: valueLabel(v) }))] })
      else if (a.type === 'bool') cols.push({ ...base, kind: 'select', options: [{ value: '', label: '' }, { value: 'true', label: 'Yes' }, { value: 'false', label: 'No' }] })
      else if (a.type === 'int') cols.push({ ...base, kind: 'int', mono: true })
      else if (a.type === 'number') cols.push({ ...base, kind: 'number', mono: true })
      else if (a.type === 'multi') cols.push({ ...base, kind: 'multi' })
      else cols.push({ ...base, kind: 'text' })
    }
    return cols
  }

  /** The column set of one row: its own category's attributes may differ from the sheet's. */
  cellApplies(row: Row, col: Column): boolean {
    const k = attrKey(col.key)
    if (!k) return true
    return !!this.#cat(row.values.category)?.attributes.some((a) => a.key === k)
  }

  /** Switch the column set. Rows following the sheet category switch with it; values of columns that still exist stay. */
  setCategory(key: string) {
    const old = this.category
    if (key === old) return
    this.category = key
    for (const row of this.rows) {
      if (row.values.category !== old) continue
      this.#setRaw(row, 'category', key)
      for (const k of Object.keys(row.values)) {
        const a = attrKey(k)
        if (!a) continue
        const attr = this.#cat(key)?.attributes.find((x) => x.key === a)
        if (!attr || !this.#fits(attr, row.values[k])) this.#setRaw(row, k, '')
      }
      this.#touchSave(row)
    }
    this.cursor.col = Math.min(this.cursor.col, this.columns.length - 1)
    this.anchor = { ...this.cursor }
  }

  #fits(a: Attribute, v: string): boolean {
    if (v === '') return true
    if (a.type === 'enum') return !!a.values?.includes(v)
    if (a.type === 'multi') return v.split(',').every((x) => a.values?.includes(x.trim()))
    return true
  }

  // ---- rows ----------------------------------------------------------------

  #blankRow(): Row {
    return {
      id: ulid(),
      values: { quantity: '1', category: this.category, place: this.defaults.place, status: this.defaults.status },
      state: 'new',
      error: '',
      created: false,
      ai: [],
      suggestions: {},
    }
  }

  /** Has the person typed anything beyond the defaults? */
  #touched(row: Row, d: Record<string, string> = this.#defaultsFor()): boolean {
    return Object.entries(row.values).some(([k, v]) => v !== '' && d[k] !== v)
  }
  #defaultsFor(): Record<string, string> {
    return { quantity: '1', category: this.category, place: this.defaults.place, status: this.defaults.status }
  }

  /** Keep one empty row at the end to type into. */
  #ensureBlank() {
    const last = this.rows[this.rows.length - 1]
    if (!last || this.#touched(last) || last.created) this.rows.push(this.#blankRow())
  }

  setDefaults(d: Partial<{ place: string; status: string }>) {
    const old = this.#defaultsFor()
    Object.assign(this.defaults, d)
    // Untouched rows follow the defaults.
    for (const r of this.rows) {
      if (!r.created && !this.#touched(r, old)) {
        r.values.place = this.defaults.place
        r.values.status = this.defaults.status
      }
    }
  }

  addPlace(p: PlaceShort) {
    this.places = { ...this.places, [p.id]: p }
  }
  setPlaces(list: PlaceShort[]) {
    const next = { ...this.places }
    for (const p of list) next[p.id] = p
    this.places = next
  }

  rowIndex(id: string) {
    return this.rows.findIndex((r) => r.id === id)
  }

  // ---- cell values ---------------------------------------------------------

  value(row: number, col: number): string {
    return this.rows[row]?.values[this.columns[col]?.key] ?? ''
  }

  /** What a cell shows: place names and option labels instead of ids. */
  display(row: Row, col: Column): string {
    const v = row.values[col.key] ?? ''
    if (col.kind === 'place') return this.places[v]?.name ?? ''
    if (col.kind === 'select') return col.options?.find((o) => o.value === v)?.label ?? v
    return v
  }

  #setRaw(row: Row, key: string, value: string) {
    if ((row.values[key] ?? '') === value) return false
    row.values[key] = value
    const rev = this.#rev.get(row.id) ?? {}
    rev[key] = (rev[key] ?? 0) + 1
    this.#rev.set(row.id, rev)
    return true
  }

  /** A person changes one cell: records undo, drops a suggestion for it, schedules saving. */
  #personSet(row: Row, key: string, value: string, changes?: Change[]): boolean {
    const before = row.values[key] ?? ''
    const wasAi = row.ai.includes(key)
    if (key === 'name' && value.trim() === '' && row.created) return false // a saved item keeps its name
    if (!this.#setRaw(row, key, value)) return false
    row.ai = row.ai.filter((k) => k !== key)
    delete row.suggestions[key]
    changes?.push({ rowId: row.id, key, before, after: value, beforeAi: wasAi })
    return true
  }

  #touchSave(row: Row) {
    if (row.created) this.#schedule(row)
  }

  #pushUndo(e: UndoEntry) {
    this.#undo.push(e)
    if (this.#undo.length > 200) this.#undo.shift()
    this.undoDepth = this.#undo.length
  }

  /** Set one cell as a person (commit of an edit, a pick, a select). */
  setCell(row: number, col: number, value: string) {
    const r = this.rows[row]
    const c = this.columns[col]
    if (!r || !c) return
    const changes: Change[] = []
    if (this.#personSet(r, c.key, this.#clean(c, value), changes)) {
      this.#afterCategoryEdit(r, c.key)
      this.#pushUndo({ kind: 'cells', changes })
      this.#touchSave(r)
      this.#ensureBlank()
    }
  }

  #clean(c: Column, v: string): string {
    return c.kind === 'select' || c.kind === 'place' ? v : v.replace(/\r?\n/g, ' ').trim()
  }

  #afterCategoryEdit(row: Row, key: string) {
    if (key !== 'category') return
    const cat = this.#cat(row.values.category)
    for (const k of Object.keys(row.values)) {
      const a = attrKey(k)
      if (!a || row.values[k] === '') continue
      const attr = cat?.attributes.find((x) => x.key === a)
      if (!attr || !this.#fits(attr, row.values[k])) this.#setRaw(row, k, '')
    }
  }

  // ---- cursor and selection ------------------------------------------------

  get selection() {
    return {
      r0: Math.min(this.cursor.row, this.anchor.row),
      r1: Math.max(this.cursor.row, this.anchor.row),
      c0: Math.min(this.cursor.col, this.anchor.col),
      c1: Math.max(this.cursor.col, this.anchor.col),
    }
  }
  inSelection(row: number, col: number) {
    const s = this.selection
    return row >= s.r0 && row <= s.r1 && col >= s.c0 && col <= s.c1 && (s.r0 !== s.r1 || s.c0 !== s.c1)
  }

  /** Move the cursor. Leaving a row saves it; leaving the name cell asks for suggestions. */
  moveTo(row: number, col: number, extend = false) {
    if (this.editing) this.commit()
    const nr = Math.max(0, Math.min(this.rows.length - 1, row))
    const nc = Math.max(0, Math.min(this.columns.length - 1, col))
    const from = this.cursor
    if (!extend && from.row === nr && from.col === nc) {
      this.anchor = { ...this.cursor }
      return
    }
    if (from.row !== nr) this.#leaveRow(from.row)
    else if (this.columns[from.col]?.key === 'name' && nc !== from.col) this.#askSuggest(this.rows[from.row])
    this.cursor = { row: nr, col: nc }
    if (!extend) this.anchor = { row: nr, col: nc }
  }

  #leaveRow(index: number) {
    const row = this.rows[index]
    if (!row) return
    const name = (row.values.name ?? '').trim()
    if (name) {
      this.#askSuggest(row)
      this.#cancelTimer(row.id)
      if (!row.created || (this.#rev.get(row.id) && this.#dirty(row).length)) this.#flush(row)
    }
  }

  /** Tab / Shift+Tab / Enter step. Past the last row a fresh row appears. */
  #step(dr: number, dc: number, wrapCols = false) {
    let { row, col } = this.cursor
    if (wrapCols) {
      col += dc
      if (col >= this.columns.length) {
        col = 0
        row += 1
      } else if (col < 0) {
        col = this.columns.length - 1
        row -= 1
      }
    } else {
      row += dr
      col += dc
    }
    if (row >= this.rows.length) {
      row = this.rows.length - 1 // a fresh row exists only once something was typed in the last one
    }
    this.moveTo(row, col)
  }

  // ---- editing -------------------------------------------------------------

  startEdit(initial?: string) {
    const { row, col } = this.cursor
    const r = this.rows[row]
    const c = this.columns[col]
    if (!r || !c || !this.cellApplies(r, c)) return
    this.editing = { row, col, draft: initial ?? (r.values[c.key] ?? ''), typed: initial !== undefined }
    this.anchor = { ...this.cursor }
  }

  /** Commit a text/select edit. Place edits are committed by the picker via `setCell`. */
  commit() {
    const e = this.editing
    if (!e) return
    this.editing = null
    const c = this.columns[e.col]
    if (c?.kind === 'place') return
    this.setCell(e.row, e.col, e.draft)
  }

  cancel() {
    this.editing = null
  }

  /** Type-ahead on a select cell that is not being edited: ("o" -> first option starting with o). */
  #typeaheadSelect(ch: string, now = Date.now()) {
    const c = this.columns[this.cursor.col]
    if (!c?.options) return
    const buf = now - this.#typeahead.at > 800 ? ch : this.#typeahead.text + ch
    this.#typeahead = { text: buf, at: now }
    const f = fold(buf)
    const hit = c.options.find((o) => o.value !== '' && fold(o.label).startsWith(f)) ?? c.options.find((o) => o.value !== '' && fold(o.value).startsWith(f))
    if (hit) this.setCell(this.cursor.row, this.cursor.col, hit.value)
  }

  // ---- keyboard ------------------------------------------------------------

  /** Returns true when the key was used (the caller then calls preventDefault). */
  handleKey(k: KeyInput, now = Date.now()): boolean {
    const mod = !!(k.metaKey || k.ctrlKey)
    const key = k.key
    if (this.editing) {
      if (key === 'Enter') {
        this.commit()
        this.#step(1, 0)
        return true
      }
      if (key === 'Tab') {
        this.commit()
        this.#step(0, k.shiftKey ? -1 : 1, true)
        return true
      }
      if (key === 'Escape') {
        this.cancel()
        return true
      }
      return false
    }
    if (mod && !k.altKey) {
      const lower = key.toLowerCase()
      if (lower === 'd') return (this.fillDown(), true)
      if (lower === 'z' && !k.shiftKey) return (this.undo(), true)
      if (key === 'Enter') return (this.acceptRow(), true)
      if (key === 'Backspace') return (this.deleteRow(this.cursor.row), true)
      return false
    }
    if (k.altKey || mod) return false
    const { row, col } = this.cursor
    switch (key) {
      case 'ArrowDown':
        this.moveTo(row + 1, col, !!k.shiftKey)
        return true
      case 'ArrowUp':
        this.moveTo(row - 1, col, !!k.shiftKey)
        return true
      case 'ArrowLeft':
        this.moveTo(row, col - 1, !!k.shiftKey)
        return true
      case 'ArrowRight':
        this.moveTo(row, col + 1, !!k.shiftKey)
        return true
      case 'PageDown':
        this.moveTo(row + 10, col)
        return true
      case 'PageUp':
        this.moveTo(row - 10, col)
        return true
      case 'Home':
        this.moveTo(row, 0)
        return true
      case 'End':
        this.moveTo(row, this.columns.length - 1)
        return true
      case 'Tab':
        if (!k.shiftKey && this.suggestionAt(row, col) !== undefined) this.acceptCell()
        this.#step(0, k.shiftKey ? -1 : 1, true)
        return true
      case 'Enter':
        this.#step(1, 0)
        return true
      case 'F2':
        this.startEdit()
        return true
      case 'Escape':
        this.anchor = { ...this.cursor }
        return true
      case 'Delete':
      case 'Backspace':
        this.clearSelection()
        return true
    }
    if (key.length === 1) {
      const c = this.columns[col]
      const r = this.rows[row]
      if (!c || !r || !this.cellApplies(r, c)) return true
      if (c.kind === 'select') this.#typeaheadSelect(key, now)
      else this.startEdit(key)
      return true
    }
    return false
  }

  clearSelection() {
    const s = this.selection
    const changes: Change[] = []
    for (let r = s.r0; r <= s.r1; r++)
      for (let c = s.c0; c <= s.c1; c++) {
        const row = this.rows[r]
        const col = this.columns[c]
        if (!row || !col || (col.key === 'name' && row.created)) continue
        const empty = col.key === 'quantity' ? '1' : ''
        this.#personSet(row, col.key, empty, changes)
        this.#touchSave(row)
      }
    if (changes.length) this.#pushUndo({ kind: 'cells', changes })
  }

  // ---- fill down, undo, paste ---------------------------------------------

  /** ⌘D: the cell above goes down. With a range, the first row of the range goes down it. */
  fillDown() {
    const s = this.selection
    const multi = s.r1 > s.r0
    const srcRow = multi ? s.r0 : s.r0 - 1
    if (srcRow < 0) return
    const src = this.rows[srcRow]
    const changes: Change[] = []
    for (let r = multi ? s.r0 + 1 : s.r0; r <= s.r1; r++)
      for (let c = s.c0; c <= s.c1; c++) {
        const col = this.columns[c]
        const row = this.rows[r]
        if (!col || !row || !this.cellApplies(row, col)) continue
        this.#personSet(row, col.key, src.values[col.key] ?? '', changes)
        this.#touchSave(row)
      }
    if (changes.length) {
      this.#pushUndo({ kind: 'cells', changes })
      this.#ensureBlank()
    }
  }

  undo(): boolean {
    const e = this.#undo.pop()
    this.undoDepth = this.#undo.length
    if (!e) return false
    if (e.kind === 'delete') {
      const at = Math.min(e.index, this.rows.length)
      this.rows.splice(at, 0, e.row)
      e.row.state = e.row.created ? 'saving' : 'new'
      if (e.row.created) this.#chain(e.row, async () => {
        try {
          await this.api.restore(e.row.id)
          e.row.state = this.#dirty(e.row).length ? 'saving' : 'saved'
          if (this.#dirty(e.row).length) this.#schedule(e.row)
        } catch (err) {
          this.#fail(e.row, err)
        }
      })
      return true
    }
    for (const ch of [...e.changes].reverse()) {
      const row = this.rows.find((r) => r.id === ch.rowId)
      if (!row) continue
      this.#setRaw(row, ch.key, ch.before)
      row.ai = row.ai.filter((k) => k !== ch.key)
      if (ch.beforeAi) row.ai.push(ch.key)
      this.#touchSave(row)
    }
    for (const id of e.addedRows ?? []) this.#removeRow(id, true)
    this.cursor.row = Math.min(this.cursor.row, this.rows.length - 1)
    this.anchor = { ...this.cursor }
    this.#ensureBlank()
    return true
  }

  /**
   * Paste clipboard text at the cursor. One column of lines becomes several rows; tab-separated text fills cells
   * across and down. Quantities in names ("3x cable") are not parsed here.
   */
  paste(text: string): boolean {
    const grid = parseClipboard(text)
    if (!grid.length || (grid.length === 1 && grid[0].length === 1 && grid[0][0] === '')) return false
    const startCol = this.cursor.col
    const changes: Change[] = []
    const addedRows: string[] = []
    const lastCol = this.columns.length
    for (let i = 0; i < grid.length; i++) {
      const ri = this.cursor.row + i
      while (ri >= this.rows.length) {
        const nr = this.#blankRow()
        this.rows.push(nr)
        addedRows.push(nr.id)
      }
      const row = this.rows[ri]
      for (let j = 0; j < grid[i].length && startCol + j < lastCol; j++) {
        const col = this.columns[startCol + j]
        if (!this.cellApplies(row, col)) continue
        const v = this.#fromText(col, grid[i][j])
        if (v === undefined) continue
        this.#personSet(row, col.key, v, changes)
      }
    }
    if (!changes.length && !addedRows.length) return false
    this.#pushUndo({ kind: 'cells', changes, addedRows })
    this.#ensureBlank()
    // Pasted rows with a name are saved as if the cursor had left them.
    for (let i = 0; i < grid.length; i++) this.#leaveRow(this.cursor.row + i)
    this.moveTo(this.cursor.row + grid.length - 1, startCol)
    return true
  }

  /** Pasted text to a cell value; undefined when it does not fit the column. */
  #fromText(col: Column, text: string): string | undefined {
    const t = text.trim()
    if (col.kind === 'select') {
      if (t === '') return ''
      const f = fold(t)
      return col.options?.find((o) => fold(o.value) === f || fold(o.label) === f)?.value
    }
    if (col.kind === 'place') {
      if (t === '') return this.defaults.place
      const all = Object.values(this.places)
      const exact = all.filter((p) => fold(p.name) === fold(t) || fold(p.path) === fold(t))
      if (exact.length === 1) return exact[0].id
      const hits = searchPlaces(all as never, t)
      return hits.length === 1 ? hits[0].id : undefined
    }
    return this.#clean(col, text)
  }

  // ---- deleting rows -------------------------------------------------------

  #removeRow(id: string, remote: boolean) {
    const i = this.rowIndex(id)
    if (i < 0) return
    const [row] = this.rows.splice(i, 1)
    this.#cancelTimer(id)
    this.#deleted.add(id)
    // Queued behind a create that may still be on its way.
    if (remote) this.#chain(row, async () => {
      if (row.created) await this.api.remove(row.id).catch(() => {})
    })
    return row
  }
  #deleted = new Set<string>()

  /** Delete a row and its draft. `undo()` brings both back. */
  deleteRow(index: number): boolean {
    const row = this.rows[index]
    if (!row) return false
    if (this.rows.length === 1 && !this.#touched(row) && !row.created) return false
    this.#removeRow(row.id, true)
    this.#deleted.delete(row.id)
    row.state = row.created ? 'saved' : 'new'
    this.#pushUndo({ kind: 'delete', row, index })
    if (!this.rows.length) this.rows.push(this.#blankRow())
    this.cursor.row = Math.min(this.cursor.row, this.rows.length - 1)
    this.anchor = { ...this.cursor }
    return true
  }

  // ---- saving --------------------------------------------------------------

  #chain(row: Row, op: () => Promise<unknown>) {
    const prev = this.#queue.get(row.id) ?? Promise.resolve()
    const next = prev.then(op, op)
    this.#queue.set(row.id, next)
    return next
  }

  #cancelTimer(id: string) {
    const t = this.#timers.get(id)
    if (t) clearTimeout(t)
    this.#timers.delete(id)
  }

  #schedule(row: Row) {
    if (!row.created) return
    this.#cancelTimer(row.id)
    if (row.state !== 'failed') row.state = 'saving'
    this.#timers.set(
      row.id,
      setTimeout(() => {
        this.#timers.delete(row.id)
        this.#flush(row)
      }, this.debounceMs),
    )
  }

  /** Column keys that changed since the last acknowledged save. */
  #dirty(row: Row): string[] {
    const rev = this.#rev.get(row.id) ?? {}
    const acked = this.#acked.get(row.id) ?? {}
    return Object.keys(rev).filter((k) => (rev[k] ?? 0) !== (acked[k] ?? 0))
  }

  /** Send what changed. Requests of one row run one after another. */
  #flush(row: Row): Promise<unknown> {
    return this.#chain(row, () => this.#send(row))
  }

  /** Wait until everything pending is sent (leaving the screen, tests). */
  async flushAll() {
    for (const row of [...this.rows]) {
      this.#cancelTimer(row.id)
      if (row.created ? this.#dirty(row).length : (row.values.name ?? '').trim()) await this.#flush(row)
    }
    await Promise.all([...this.#queue.values()])
  }

  retry(index: number) {
    const row = this.rows[index]
    if (!row || row.state !== 'failed') return
    row.error = ''
    row.state = 'saving'
    return this.#flush(row)
  }

  #body(row: Row, keys: string[], create: boolean): Record<string, unknown> | string {
    const body: Record<string, unknown> = {}
    const attrs: Record<string, unknown> = {}
    const aiPaths: string[] = []
    const cat = this.#cat(row.values.category)
    for (const k of keys) {
      const v = (row.values[k] ?? '').trim()
      const a = attrKey(k)
      if (a) {
        const attr = cat?.attributes.find((x) => x.key === a)
        if (!attr) {
          if (!create) attrs[a] = null // cleared by a category change
          continue
        }
        if (v === '') {
          if (!create) attrs[a] = null
          continue
        }
        const conv = this.#attrValue(attr, v)
        if (conv === undefined) return `${humanize(a)}: not a valid ${attr.type === 'int' ? 'whole number' : attr.type}.`
        attrs[a] = conv
        if (row.ai.includes(k)) aiPaths.push(`attributes.${a}`)
        continue
      }
      switch (k) {
        case 'name':
          if (v) body.name = v
          break
        case 'quantity': {
          if (v === '') break
          if (!/^\d+$/.test(v)) return 'Quantity must be a whole number.'
          body.quantity = Number(v)
          break
        }
        case 'place':
          if (v || !create) body.place_id = v || null
          break
        case 'category':
        case 'status':
          if (v) body[k] = v
          break
        default:
          if (v || !create) body[k] = v || null
      }
      if (row.ai.includes(k) && k !== 'place' && k !== 'status') aiPaths.push(k)
    }
    if (Object.keys(attrs).length) body.attributes = attrs
    if (this.sendAiFields && aiPaths.length) body.ai_fields = aiPaths
    return body
  }

  #attrValue(a: Attribute, v: string): unknown {
    switch (a.type) {
      case 'int':
        return /^-?\d+$/.test(v) ? Number(v) : undefined
      case 'number': {
        const n = Number(v.replace(',', '.'))
        return Number.isFinite(n) ? n : undefined
      }
      case 'bool':
        return v === 'true' ? true : v === 'false' ? false : undefined
      case 'multi':
        return v.split(',').map((x) => x.trim()).filter(Boolean)
      default:
        return v
    }
  }

  async #send(row: Row) {
    if (this.#deleted.has(row.id) || this.rowIndex(row.id) < 0) return
    const create = !row.created
    const rev = this.#rev.get(row.id) ?? {}
    const snapshot = { ...rev }
    const keys = create ? Object.keys(row.values).filter((k) => (row.values[k] ?? '') !== '') : this.#dirty(row)
    if (create && !(row.values.name ?? '').trim()) return
    if (!create && !keys.length) {
      row.state = 'saved'
      return
    }
    const body = this.#body(row, keys, create)
    if (typeof body === 'string') {
      row.state = 'failed'
      row.error = body
      return
    }
    row.state = 'saving'
    row.error = ''
    try {
      const item = create ? await this.api.create({ id: row.id, draft: true, ...body }) : await this.api.patch(row.id, body)
      // Only acknowledge what was sent; newer edits of the same cell stay dirty and win.
      const acked = this.#acked.get(row.id) ?? {}
      for (const k of create ? Object.keys(snapshot) : keys) acked[k] = snapshot[k] ?? 0
      this.#acked.set(row.id, acked)
      row.created = true
      this.#apply(row, item, snapshot)
      if (this.#dirty(row).length) this.#schedule(row)
      else row.state = 'saved'
    } catch (e) {
      this.#fail(row, e)
    }
  }

  /** Take the server's version of a cell unless the person changed it since the request left. */
  #apply(row: Row, item: Item, sent: Record<string, number>) {
    if (!item || typeof item !== 'object') return
    const rev = this.#rev.get(row.id) ?? {}
    const fresh = (k: string) => (rev[k] ?? 0) === (sent[k] ?? 0)
    const put = (k: string, v: string) => {
      if (fresh(k) && row.values[k] !== undefined && row.values[k] !== v) row.values[k] = v
    }
    if (typeof item.name === 'string' && item.name) put('name', item.name)
    if (typeof item.quantity === 'number') put('quantity', String(item.quantity))
    if (typeof item.category === 'string') put('category', item.category)
    if (typeof item.status === 'string') put('status', item.status)
    if (item.brand !== undefined && row.values.brand) put('brand', item.brand ?? '')
    if (item.model !== undefined && row.values.model) put('model', item.model ?? '')
    if (item.place) {
      this.addPlace(item.place)
      put('place', item.place.id)
    }
  }

  #fail(row: Row, e: unknown) {
    const x = explain(e)
    row.state = 'failed'
    row.error = x.message || Object.values(x.fields)[0] || 'Could not save.'
  }

  // ---- suggestions (J7.2 seam) --------------------------------------------

  suggestionAt(row: number, col: number): string | undefined {
    const r = this.rows[row]
    const c = this.columns[col]
    if (!r || !c) return undefined
    const s = r.suggestions[c.key]
    return s && (r.values[c.key] ?? '') === '' ? s : undefined
  }

  #askSuggest(row: Row | undefined) {
    if (!this.suggest || !row) return
    const name = (row.values.name ?? '').trim()
    if (!name || this.#suggestedFor.get(row.id) === name) return
    this.#suggestedFor.set(row.id, name)
    this.#suggestPending.add(row.id)
    if (this.#suggestScheduled) return
    this.#suggestScheduled = true
    // Rows left in the same tick go out as one request.
    queueMicrotask(() => this.#runSuggest())
  }

  async #runSuggest() {
    this.#suggestScheduled = false
    const ids = [...this.#suggestPending]
    this.#suggestPending.clear()
    const asked = new Map<string, string>()
    const input: SuggestRow[] = []
    for (const id of ids) {
      const row = this.rows.find((r) => r.id === id)
      if (!row) continue
      const name = (row.values.name ?? '').trim()
      asked.set(id, name)
      input.push({ id, name, values: { ...row.values } })
    }
    if (!input.length || !this.suggest) return
    let out: SuggestResult[]
    try {
      out = await this.suggest(input)
    } catch {
      for (const id of asked.keys()) this.#suggestedFor.delete(id)
      return
    }
    for (const res of out ?? []) {
      const row = this.rows.find((r) => r.id === res.id)
      // An edited name makes the old answer wrong.
      if (!row || (row.values.name ?? '').trim() !== asked.get(res.id)) continue
      for (const [key, text] of Object.entries(res.cells ?? {})) {
        if (!text || !this.columns.some((c) => c.key === key)) continue
        if ((row.values[key] ?? '') !== '') continue // never replaces a typed value
        row.suggestions[key] = text
      }
    }
  }

  /** Tab: accept the suggestion under the cursor. */
  acceptCell(): boolean {
    return this.#accept(this.cursor.row, [this.columns[this.cursor.col]?.key])
  }
  /** ⌘Enter: accept every suggestion of the cursor's row. */
  acceptRow(): boolean {
    return this.#accept(this.cursor.row, Object.keys(this.rows[this.cursor.row]?.suggestions ?? {}))
  }

  #accept(index: number, keys: (string | undefined)[]): boolean {
    const row = this.rows[index]
    if (!row) return false
    const changes: Change[] = []
    const accepted: string[] = []
    for (const key of keys) {
      if (!key) continue
      const text = row.suggestions[key]
      if (!text || (row.values[key] ?? '') !== '') continue
      const before = row.values[key] ?? ''
      this.#setRaw(row, key, text)
      delete row.suggestions[key]
      if (!row.ai.includes(key)) row.ai.push(key)
      changes.push({ rowId: row.id, key, before, after: text })
      accepted.push(key)
    }
    if (!accepted.length) return false
    this.#pushUndo({ kind: 'cells', changes })
    for (const k of accepted) this.onAccept?.(row.id, k)
    this.#touchSave(row)
    this.#ensureBlank()
    return true
  }
}
