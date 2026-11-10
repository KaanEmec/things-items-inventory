import { describe, expect, it } from 'vitest'
import type { Catalog } from '../inventory/types'
import { Describe, splitText, toSheetRow } from './textrows.svelte'
import { Sheet, type SheetApi } from './sheet.svelte'

const catalog: Catalog = {
  statuses: ['stored', 'packed'],
  conditions: [],
  categories: [{ key: 'other', attributes: [] }, { key: 'gadget', attributes: [] }],
}
const make = () => {
  const created: Record<string, any>[] = []
  const api: SheetApi = {
    create: async (f) => (created.push(f), { id: f.id } as any),
    patch: async (id) => ({ id }) as any,
    remove: async () => ({}),
    restore: async () => ({}),
  }
  return { s: new Sheet(catalog, { api }), created }
}

describe('splitText', () => {
  it('keeps short text whole and cuts long text at line breaks', () => {
    expect(splitText('a\nb', 10)).toEqual(['a\nb'])
    expect(splitText('aaaa\nbbbb\ncccc', 9)).toEqual(['aaaa\nbbbb', 'cccc'])
    expect(splitText('x'.repeat(25), 10).map((p) => p.length)).toEqual([10, 10, 5])
  })
})

describe('Describe', () => {
  it('sends each part, shows rows, and adds them all as drafts', async () => {
    const calls: string[] = []
    const d = new Describe(async (t) => (calls.push(t), { ai: false, rows: [{ source: t, values: { name: t, quantity: 2 }, confidence: {}, place_id: null }] }))
    d.text = 'a\n' + 'b'.repeat(12000)
    await d.run()
    expect(calls).toHaveLength(2)
    expect(d.rows).toHaveLength(2)
    const { s, created } = make()
    d.addAll(s)
    await s.flushAll()
    expect(created).toHaveLength(2)
    expect(created[0]).toMatchObject({ name: 'a', quantity: 2, draft: true })
    expect(d.rows).toHaveLength(0)
  })
  it('maps purchase fields to extra', () => {
    const r = toSheetRow({ source: '', values: { name: 'x', price_minor: 500, seller: 'S' }, confidence: {}, place_id: 'p1' }, true)
    expect(r.extra).toEqual({ purchase: { price_minor: 500, seller: 'S' } })
    expect(r.place).toBe('p1')
  })
})

describe('phone list and smart defaults', () => {
  it('addTyped saves a draft at once and carries category and status from the row above', async () => {
    const { s, created } = make()
    s.addTyped('Phone')
    s.setValue(s.named[0].id, 'category', 'gadget')
    s.setValue(s.named[0].id, 'status', 'packed')
    s.addTyped('Charger')
    await s.flushAll()
    expect(s.named.map((r) => r.values.name)).toEqual(['Phone', 'Charger'])
    expect(created[1]).toMatchObject({ name: 'Charger', category: 'gadget', status: 'packed' })
  })
  it('a box as the place sets packed, leaving it sets stored', () => {
    const { s } = make()
    s.setPlaceDefault({ id: 'b', kind: 'box', name: 'Box 1' } as any)
    expect(s.defaults.status).toBe('packed')
    s.setPlaceDefault({ id: 'r', kind: 'room', name: 'Room' } as any)
    expect(s.defaults.status).toBe('stored')
  })
})

import { offlineApi } from './offline'
import { ApiError } from '../api'
describe('offline lines', () => {
  it('a line created offline is queued, edited in the queue and removed from it', async () => {
    const lines = new Map<string, Record<string, unknown>>()
    const q = {
      addLine: async (id: string, l: Record<string, unknown>) => (lines.set(id, l), {} as any),
      updateLine: async (id: string, f: Record<string, unknown>) => (lines.has(id) ? (lines.set(id, { ...lines.get(id), ...f }), true) : false),
      removeLine: async (id: string) => lines.delete(id),
    }
    const base: SheetApi = {
      create: async () => { throw new ApiError(0, 'network', 'offline') },
      patch: async () => ({ id: 'remote' }) as any,
      remove: async () => ({}),
      restore: async () => ({}),
    }
    const api = offlineApi(q, base)
    expect(await api.create({ id: 'L1', name: 'Scarf' })).toMatchObject({ id: 'L1' })
    await api.patch('L1', { quantity: 3 })
    expect(lines.get('L1')).toMatchObject({ name: 'Scarf', quantity: 3 })
    expect(await api.patch('other', {})).toMatchObject({ id: 'remote' })
    await api.remove('L1')
    expect(lines.size).toBe(0)
  })
})

import { waitForJob } from './csvimport'
describe('waitForJob', () => {
  it('polls until the job is over', async () => {
    const states = [{ state: 'queued' }, { state: 'running' }, { state: 'done', result: { added: 2, failed: 0 } }] as any[]
    const seen: string[] = []
    const last = await waitForJob('j', (s) => seen.push(s.state), { intervalMs: 0, get: async () => states.shift() })
    expect(seen).toEqual(['queued', 'running', 'done'])
    expect(last.result?.added).toBe(2)
  })
})
