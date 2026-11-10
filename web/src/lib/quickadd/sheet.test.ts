import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Catalog, Item } from '../inventory/types'
import { Sheet, parseClipboard, type SheetApi, type SuggestFn } from './sheet.svelte'

const catalog: Catalog = {
  statuses: ['stored', 'in_use', 'to_sell'],
  conditions: [],
  categories: [
    { key: 'other', attributes: [{ key: 'color', type: 'text' }] },
    { key: 'gadget', attributes: [{ key: 'color', type: 'text' }, { key: 'storage_gb', type: 'int' }, { key: 'connector', type: 'enum', values: ['usb_c', 'micro_usb'] }] },
    { key: 'clothing', attributes: [{ key: 'size', type: 'text' }] },
  ],
}

interface Call {
  op: 'create' | 'patch' | 'remove' | 'restore'
  id?: string
  body?: Record<string, any>
  resolve: (item?: Partial<Item>) => void
  reject: (e: unknown) => void
}

function fakeApi() {
  const calls: Call[] = []
  const mk = (op: Call['op'], id?: string, body?: Record<string, any>) =>
    new Promise<any>((resolve, reject) => calls.push({ op, id, body, resolve: (it) => resolve(it ?? { id }), reject }))
  const api: SheetApi = {
    create: (f) => mk('create', f.id as string, f),
    patch: (id, f) => mk('patch', id, f),
    remove: (id) => mk('remove', id),
    restore: (id) => mk('restore', id),
  }
  return { api, calls }
}

const tick = () => new Promise((r) => setTimeout(r, 0))
const press = (s: Sheet, ...keys: string[]) => keys.forEach((k) => s.handleKey({ key: k }))
const type = (s: Sheet, text: string) => {
  for (const ch of text) s.handleKey({ key: ch })
}

function make(opts: ConstructorParameters<typeof Sheet>[1] = {}) {
  const f = fakeApi()
  const s = new Sheet(catalog, { api: f.api, debounceMs: 50, ...opts })
  return { s, ...f }
}

describe('columns', () => {
  it('lists the base columns then the category attributes', () => {
    const { s } = make()
    expect(s.columns.map((c) => c.key)).toEqual(['name', 'quantity', 'category', 'brand', 'model', 'place', 'status', 'attr.color'])
    s.setCategory('gadget')
    expect(s.columns.map((c) => c.key).slice(7)).toEqual(['attr.color', 'attr.storage_gb', 'attr.connector'])
  })

  it('keeps values of columns that still exist and drops the rest', () => {
    const { s } = make()
    s.setCategory('gadget')
    s.moveTo(0, 0)
    s.setCell(0, 0, 'Phone')
    s.setCell(0, 7, 'black')
    s.setCell(0, 8, '128')
    s.setCategory('clothing')
    expect(s.rows[0].values.name).toBe('Phone')
    expect(s.rows[0].values['attr.color']).toBeUndefined
    expect(s.rows[0].values['attr.storage_gb']).toBe('')
    s.setCategory('other')
    s.setCell(0, 7, 'red')
    s.setCategory('gadget')
    expect(s.rows[0].values['attr.color']).toBe('red')
    expect(s.rows[0].values.category).toBe('gadget')
  })

  it('new rows start with the header defaults', () => {
    const { s } = make()
    s.setDefaults({ place: 'P1', status: 'in_use' })
    expect(s.rows[0].values.place).toBe('P1')
    s.setCell(0, 0, 'a')
    expect(s.rows[1].values).toMatchObject({ place: 'P1', status: 'in_use', quantity: '1' })
  })
})

describe('navigation and editing', () => {
  it('moves with arrows, Tab wraps to the next row, Enter goes down', () => {
    const { s } = make()
    s.setCell(0, 0, 'a')
    s.setCell(1, 0, 'b')
    press(s, 'ArrowRight', 'ArrowRight')
    expect(s.cursor).toEqual({ row: 0, col: 2 })
    press(s, 'ArrowLeft', 'ArrowUp')
    expect(s.cursor).toEqual({ row: 0, col: 1 })
    s.moveTo(0, s.columns.length - 1)
    press(s, 'Tab')
    expect(s.cursor).toEqual({ row: 1, col: 0 })
    s.handleKey({ key: 'Tab', shiftKey: true })
    expect(s.cursor).toEqual({ row: 0, col: s.columns.length - 1 })
    s.moveTo(0, 3)
    press(s, 'Enter')
    expect(s.cursor).toEqual({ row: 1, col: 3 })
  })

  it('typing starts editing, Enter commits and moves down, Esc cancels', () => {
    const { s } = make()
    type(s, 'c')
    expect(s.editing?.draft).toBe('c')
    s.editing!.draft = 'cable'
    press(s, 'Enter')
    expect(s.editing).toBeNull()
    expect(s.rows[0].values.name).toBe('cable')
    expect(s.cursor.row).toBe(1)
    s.moveTo(0, 0)
    press(s, 'F2')
    expect(s.editing?.draft).toBe('cable')
    s.editing!.draft = 'other'
    press(s, 'Escape')
    expect(s.rows[0].values.name).toBe('cable')
  })

  it('Tab while editing commits and moves right', () => {
    const { s } = make()
    type(s, 'x')
    press(s, 'Tab')
    expect(s.rows[0].values.name).toBe('x')
    expect(s.cursor).toEqual({ row: 0, col: 1 })
  })

  it('a new blank row follows once the last one has content', () => {
    const { s } = make()
    expect(s.rows).toHaveLength(1)
    s.setCell(0, 0, 'a')
    expect(s.rows).toHaveLength(2)
    s.setCell(0, 3, 'brand')
    expect(s.rows).toHaveLength(2)
  })

  it('type-ahead on select cells chooses an option', () => {
    const { s } = make()
    s.moveTo(0, 6)
    s.handleKey({ key: 't' }, 1000)
    expect(s.rows[0].values.status).toBe('to_sell')
    s.handleKey({ key: 'i' }, 2000)
    expect(s.rows[0].values.status).toBe('in_use')
    s.moveTo(0, 2)
    s.handleKey({ key: 'g' }, 5000)
    expect(s.rows[0].values.category).toBe('gadget')
  })
})

describe('fill down and undo', () => {
  it('fills the cell above down and undoes it', () => {
    const { s } = make()
    s.setCell(0, 3, 'Sony')
    s.setCell(1, 0, 'x')
    s.moveTo(1, 3)
    s.handleKey({ key: 'd', metaKey: true })
    expect(s.rows[1].values.brand).toBe('Sony')
    s.handleKey({ key: 'z', ctrlKey: true })
    expect(s.rows[1].values.brand ?? '').toBe('')
  })

  it('fills a selected range from its first row', () => {
    const { s } = make()
    s.setCell(0, 0, 'a')
    s.setCell(1, 0, 'b')
    s.setCell(2, 0, 'c')
    s.setCell(0, 3, 'Sony')
    s.moveTo(0, 3)
    s.handleKey({ key: 'ArrowDown', shiftKey: true })
    s.handleKey({ key: 'ArrowDown', shiftKey: true })
    s.handleKey({ key: 'd', metaKey: true })
    expect(s.rows.slice(0, 3).map((r) => r.values.brand)).toEqual(['Sony', 'Sony', 'Sony'])
    s.undo()
    expect(s.rows[1].values.brand ?? '').toBe('')
    expect(s.rows[2].values.brand ?? '').toBe('')
  })

  it('undoes a cell edit', () => {
    const { s } = make()
    s.setCell(0, 0, 'a')
    s.setCell(0, 0, 'b')
    s.undo()
    expect(s.rows[0].values.name).toBe('a')
  })
})

describe('paste', () => {
  it('parses clipboard text', () => {
    expect(parseClipboard('a\tb\r\nc\td\r\n')).toEqual([['a', 'b'], ['c', 'd']])
  })

  it('lines pasted into the name column become rows', () => {
    const { s } = make()
    expect(s.paste('hdmi cable\nusb hub\n3x cable')).toBe(true)
    expect(s.rows.map((r) => r.values.name).filter(Boolean)).toEqual(['hdmi cable', 'usb hub', '3x cable'])
    expect(s.rows[2].values.quantity).toBe('1') // quantity is not parsed from the name
    s.undo()
    expect(s.rows.every((r) => !r.values.name)).toBe(true)
  })

  it('tab-separated text fills cells across and down', () => {
    const { s } = make()
    s.paste('Cable\t2\tgadget\tAnker\tA1\n\tx\n\nHub\t1\tClothing\tUgreen')
    expect(s.rows[0].values).toMatchObject({ name: 'Cable', quantity: '2', category: 'gadget', brand: 'Anker', model: 'A1' })
    expect(s.rows[3].values).toMatchObject({ name: 'Hub', category: 'clothing', brand: 'Ugreen' })
    expect(s.rows[1].values.quantity).toBe('x')
  })

  it('ignores select values that match no option', () => {
    const { s } = make()
    s.moveTo(0, 6)
    s.paste('nonsense')
    expect(s.rows[0].values.status).toBe('stored')
    s.paste('To sell / donate')
    expect(s.rows[0].values.status).toBe('to_sell')
  })
})

describe('saving', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it('creates a draft when the cursor leaves a row with a name, with a client id', async () => {
    const { s, calls } = make()
    s.setCell(0, 0, 'Cable')
    s.setCell(0, 3, 'Anker')
    expect(calls).toHaveLength(0)
    expect(s.rows[0].state).toBe('new')
    s.moveTo(1, 0)
    await vi.advanceTimersByTimeAsync(0)
    expect(calls).toHaveLength(1)
    expect(calls[0].op).toBe('create')
    expect(calls[0].body).toMatchObject({ id: s.rows[0].id, draft: true, name: 'Cable', brand: 'Anker', quantity: 1, category: 'other', status: 'stored' })
    expect(s.rows[0].state).toBe('saving')
    calls[0].resolve({ id: s.rows[0].id, name: 'Cable' })
    await vi.advanceTimersByTimeAsync(0)
    expect(s.rows[0].state).toBe('saved')
    expect(s.added).toBe(1)
  })

  it('does not save a row without a name', async () => {
    const { s, calls } = make()
    s.setCell(0, 3, 'Anker')
    s.moveTo(1, 0)
    await vi.advanceTimersByTimeAsync(100)
    expect(calls).toHaveLength(0)
  })

  it('patches only changed fields, debounced', async () => {
    const { s, calls } = make()
    s.setCell(0, 0, 'Cable')
    s.moveTo(1, 0)
    await vi.advanceTimersByTimeAsync(0)
    calls[0].resolve()
    await vi.advanceTimersByTimeAsync(0)
    s.setCell(0, 3, 'A')
    s.setCell(0, 3, 'An')
    s.setCell(0, 4, 'M1')
    await vi.advanceTimersByTimeAsync(49)
    expect(calls).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(2)
    expect(calls).toHaveLength(2)
    expect(calls[1].op).toBe('patch')
    expect(calls[1].body).toEqual({ brand: 'An', model: 'M1' })
  })

  it('an answer for an older edit does not overwrite a newer cell, and the newer edit is sent next', async () => {
    const { s, calls } = make()
    s.setCell(0, 0, 'Cable')
    s.moveTo(1, 0)
    await vi.advanceTimersByTimeAsync(0)
    s.setCell(0, 0, 'Cable 2m') // newer edit while the create is on its way
    calls[0].resolve({ id: s.rows[0].id, name: 'Cable' }) // answer carries the old name
    await vi.advanceTimersByTimeAsync(0)
    expect(s.rows[0].values.name).toBe('Cable 2m')
    expect(s.rows[0].state).toBe('saving')
    await vi.advanceTimersByTimeAsync(60)
    expect(calls[1].op).toBe('patch')
    expect(calls[1].body).toEqual({ name: 'Cable 2m' })
    calls[1].resolve({ id: s.rows[0].id, name: 'Cable 2m' })
    await vi.advanceTimersByTimeAsync(0)
    expect(s.rows[0].values.name).toBe('Cable 2m')
    expect(s.rows[0].state).toBe('saved')
  })

  it('requests of one row never overlap', async () => {
    const { s, calls } = make({ debounceMs: 0 })
    s.setCell(0, 0, 'A')
    s.moveTo(1, 0)
    await vi.advanceTimersByTimeAsync(0)
    s.setCell(0, 3, 'B')
    await vi.advanceTimersByTimeAsync(10)
    expect(calls).toHaveLength(1) // patch waits for the create
    calls[0].resolve()
    await vi.advanceTimersByTimeAsync(10)
    expect(calls).toHaveLength(2)
  })

  it('failed rows can be retried with the same id', async () => {
    const { s, calls } = make()
    s.setCell(0, 0, 'A')
    s.moveTo(1, 0)
    await vi.advanceTimersByTimeAsync(0)
    calls[0].reject(new Error('x'))
    await vi.advanceTimersByTimeAsync(0)
    expect(s.rows[0].state).toBe('failed')
    expect(s.failed).toBe(1)
    s.retry(0)
    await vi.advanceTimersByTimeAsync(0)
    expect(calls[1].op).toBe('create')
    expect(calls[1].id).toBe(calls[0].id)
  })

  it('sends attributes as typed values and a clear as null', async () => {
    const { s, calls } = make()
    s.setCategory('gadget')
    s.setCell(0, 0, 'Phone')
    s.setCell(0, 8, '128')
    s.setCell(0, 9, 'usb_c')
    s.moveTo(1, 0)
    await vi.advanceTimersByTimeAsync(0)
    expect(calls[0].body).toMatchObject({ category: 'gadget', attributes: { storage_gb: 128, connector: 'usb_c' } })
    calls[0].resolve()
    await vi.advanceTimersByTimeAsync(0)
    s.setCell(0, 8, '')
    await vi.advanceTimersByTimeAsync(60)
    expect(calls[1].body).toEqual({ attributes: { storage_gb: null } })
  })

  it('deleting removes the draft and undo restores it', async () => {
    const { s, calls } = make()
    s.setCell(0, 0, 'A')
    s.moveTo(1, 0)
    await vi.advanceTimersByTimeAsync(0)
    calls[0].resolve()
    await vi.advanceTimersByTimeAsync(0)
    s.deleteRow(0)
    await vi.advanceTimersByTimeAsync(0)
    expect(calls[1].op).toBe('remove')
    calls[1].resolve()
    expect(s.rows.some((r) => r.values.name === 'A')).toBe(false)
    s.undo()
    await vi.advanceTimersByTimeAsync(0)
    expect(calls[2].op).toBe('restore')
    expect(s.rows[0].values.name).toBe('A')
  })

  it('flushAll sends everything that left its row', async () => {
    const { s, calls } = make()
    s.setCell(0, 0, 'A')
    s.setCell(1, 0, 'B')
    const p = s.flushAll()
    await vi.advanceTimersByTimeAsync(0)
    calls[0].resolve()
    await vi.advanceTimersByTimeAsync(0)
    calls[1].resolve()
    await p
    expect(calls.map((c) => c.op)).toEqual(['create', 'create'])
  })
})

describe('suggestions', () => {
  const answer = (cells: Record<string, string>): SuggestFn => async (rows) => rows.map((r) => ({ id: r.id, cells }))

  it('asks when the name cell is left, batching rows left together, and shows ghost text in empty cells only', async () => {
    const suggest = vi.fn(answer({ brand: 'Sony', model: 'WH-1000XM4', quantity: '9' }))
    const { s } = make({ suggest })
    s.paste('wh1000xm4 black\nanker cable')
    await tick()
    expect(suggest).toHaveBeenCalledTimes(1)
    expect(suggest.mock.calls[0][0].map((r) => r.name)).toEqual(['wh1000xm4 black', 'anker cable'])
    expect(s.suggestionAt(0, 3)).toBe('Sony')
    expect(s.suggestionAt(0, 1)).toBeUndefined() // quantity already has a value
  })

  it('Tab accepts the suggestion under the cursor and records it', async () => {
    const accepted: string[] = []
    const { s } = make({ suggest: answer({ brand: 'Sony', model: 'XM4' }), onAccept: (_, k) => accepted.push(k) })
    s.setCell(0, 0, 'wh1000xm4')
    s.moveTo(0, 3) // leaves the name cell
    await tick()
    expect(s.suggestionAt(0, 3)).toBe('Sony')
    press(s, 'Tab')
    expect(s.rows[0].values.brand).toBe('Sony')
    expect(s.rows[0].ai).toEqual(['brand'])
    expect(accepted).toEqual(['brand'])
    expect(s.cursor.col).toBe(4)
    s.undo()
    expect(s.rows[0].values.brand ?? '').toBe('')
    expect(s.rows[0].ai).toEqual([])
  })

  it('Cmd+Enter accepts the whole row; typing replaces a suggestion', async () => {
    const { s } = make({ suggest: answer({ brand: 'Sony', model: 'XM4' }) })
    s.setCell(0, 0, 'wh1000xm4')
    s.moveTo(0, 3)
    await tick()
    s.moveTo(0, 4)
    type(s, 'M') // starts editing; the input owns the rest
    s.editing!.draft = 'Mine'
    press(s, 'Enter')
    expect(s.rows[0].values.model).toBe('Mine')
    expect(s.rows[0].suggestions.model).toBeUndefined()
    s.moveTo(0, 3)
    s.handleKey({ key: 'Enter', metaKey: true })
    expect(s.rows[0].values.brand).toBe('Sony')
    expect(s.rows[0].ai).toEqual(['brand']) // typed model is the person's
  })

  it('drops an answer for a name that was edited meanwhile', async () => {
    let release!: () => void
    const suggest: SuggestFn = (rows) => new Promise((r) => (release = () => r(rows.map((x) => ({ id: x.id, cells: { brand: 'Old' } })))))
    const { s } = make({ suggest })
    s.setCell(0, 0, 'wh1000')
    s.moveTo(0, 3)
    await tick()
    s.setCell(0, 0, 'something else')
    release()
    await tick()
    expect(s.suggestionAt(0, 3)).toBeUndefined()
  })

  it('does nothing without a suggest function', async () => {
    const { s } = make()
    s.setCell(0, 0, 'a')
    s.moveTo(0, 3)
    await tick()
    expect(s.rows[0].suggestions).toEqual({})
    expect(s.acceptRow()).toBe(false)
  })

  it('accepted cells go to the server as ai_fields when enabled', async () => {
    vi.useFakeTimers()
    try {
      const { s, calls } = make({ suggest: answer({ brand: 'Sony' }), sendAiFields: true })
      s.setCell(0, 0, 'wh')
      s.moveTo(0, 3)
      await vi.advanceTimersByTimeAsync(0)
      s.acceptCell()
      s.moveTo(1, 0)
      await vi.advanceTimersByTimeAsync(0)
      expect(calls[0].body).toMatchObject({ brand: 'Sony', ai_fields: ['brand'] })
    } finally {
      vi.useRealTimers()
    }
  })

  it('a suggested category fills the default, switches the attribute columns, and lists attributes only after it', async () => {
    const { s } = make({ suggest: answer({ category: 'gadget', brand: 'Sony', 'attr.storage_gb': '128' }) })
    s.setCell(0, 0, 'wh1000xm4')
    s.moveTo(0, 3)
    await tick()
    expect(s.suggestionAt(0, 2)).toBe('gadget') // shown although the cell holds the default
    expect(s.display(s.rows[0], s.columns[2])).toBe('') // the ghost replaces the default in the cell
    expect(s.columns.some((c) => c.key === 'attr.storage_gb')).toBe(false)
    s.moveTo(0, 2)
    s.handleKey({ key: 'Enter', metaKey: true }) // the whole row
    expect(s.rows[0].values).toMatchObject({ category: 'gadget', brand: 'Sony', 'attr.storage_gb': '128' })
    expect(s.rows[0].ai.sort()).toEqual(['attr.storage_gb', 'brand', 'category'])
    expect(s.category).toBe('gadget')
    expect(s.columns.some((c) => c.key === 'attr.storage_gb')).toBe(true)
  })

  it('does not ask for or take a category once the person picked one', async () => {
    const asked: Record<string, string>[] = []
    const suggest: SuggestFn = async (rows) => {
      asked.push(rows[0].values)
      return rows.map((r) => ({ id: r.id, cells: { category: 'gadget', brand: 'Sony' } }))
    }
    const { s } = make({ suggest })
    s.setCategory('clothing') // the person picked for the sheet
    s.setCell(0, 0, 'jacket')
    s.moveTo(0, 3)
    await tick()
    expect(asked[0].category).toBe('clothing')
    expect(s.suggestionAt(0, 2)).toBeUndefined()
    s.handleKey({ key: 'Enter', metaKey: true })
    expect(s.rows[0].values.category).toBe('clothing')
    expect(s.category).toBe('clothing')

    // A row category picked by typing counts too.
    const t = make({ suggest })
    t.s.setCell(0, 0, 'thing')
    t.s.setCell(0, 2, 'gadget')
    t.s.moveTo(0, 3)
    await tick()
    expect(asked[1].category).toBe('gadget')
    expect(t.s.suggestionAt(0, 2)).toBeUndefined()
    expect(t.s.category).toBe('other')
  })

  it('sends accepted category and attribute as ai_fields', async () => {
    vi.useFakeTimers()
    try {
      const { s, calls } = make({ suggest: answer({ category: 'gadget', 'attr.storage_gb': '128' }), sendAiFields: true })
      s.setCell(0, 0, 'phone')
      s.moveTo(0, 3)
      await vi.advanceTimersByTimeAsync(0)
      s.moveTo(0, 2)
      s.handleKey({ key: 'Enter', metaKey: true })
      s.moveTo(1, 0)
      await vi.advanceTimersByTimeAsync(0)
      expect(calls[0].body).toMatchObject({ category: 'gadget', attributes: { storage_gb: 128 } })
      expect((calls[0].body!.ai_fields as string[]).sort()).toEqual(['attributes.storage_gb', 'category'])
    } finally {
      vi.useRealTimers()
    }
  })
})
