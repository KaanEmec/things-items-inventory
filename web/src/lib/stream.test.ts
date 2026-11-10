import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { ApiError } from './api'
import { createStream, type StreamMessage } from './stream.svelte'

class Fake {
  static all: Fake[] = []
  handlers: Record<string, ((e: any) => void)[]> = {}
  closed = false
  constructor(public url: string) {
    Fake.all.push(this)
  }
  addEventListener(t: string, fn: (e: any) => void) {
    ;(this.handlers[t] ??= []).push(fn)
  }
  close() {
    this.closed = true
  }
  emit(t: string, data?: unknown) {
    for (const h of this.handlers[t] ?? []) h({ data: JSON.stringify(data) })
  }
}

beforeEach(() => {
  Fake.all = []
  vi.useFakeTimers()
})
afterEach(() => vi.useRealTimers())

const make = (probe: () => Promise<unknown> = async () => ({})) =>
  createStream({ open: (u) => new Fake(u), probe, burstMs: 100, burstMaxMs: 500, backoff: () => 1000 })

test('dispatches by type and unsubscribes', () => {
  const s = make()
  s.start()
  const got: string[] = []
  const off = s.on('item', (m) => got.push(m.id))
  s.on(['place', 'job'], (m) => got.push(`${m.type}:${m.id}`))
  const f = Fake.all[0]
  f.emit('open')
  expect(s.connected).toBe(true)
  f.emit('event', { type: 'item', id: 'a', kind: 'moved' })
  f.emit('event', { type: 'place', id: 'p', kind: 'edited' })
  f.emit('job', { type: 'job', id: 'j', kind: 'running' })
  off()
  f.emit('event', { type: 'item', id: 'b', kind: 'moved' })
  expect(got).toEqual(['a', 'place:p', 'job:j'])
})

test('a burst is delivered once, with every message', () => {
  const s = make()
  s.start()
  const calls: StreamMessage[][] = []
  s.onBurst('item', (b) => calls.push(b))
  for (const id of ['a', 'b', 'c']) Fake.all[0].emit('event', { type: 'item', id, kind: 'moved' })
  vi.advanceTimersByTime(99)
  expect(calls).toHaveLength(0)
  vi.advanceTimersByTime(2)
  expect(calls).toHaveLength(1)
  expect(calls[0].map((m) => m.id)).toEqual(['a', 'b', 'c'])
})

test('a steady stream still flushes after the maximum wait', () => {
  const s = make()
  s.start()
  const calls: StreamMessage[][] = []
  s.onBurst('item', (b) => calls.push(b))
  for (let i = 0; i < 8; i++) {
    Fake.all[0].emit('event', { type: 'item', id: String(i), kind: 'edited' })
    vi.advanceTimersByTime(80)
  }
  expect(calls.length).toBeGreaterThanOrEqual(1)
})

test('stop closes the source; a dropped one reconnects with a resync', async () => {
  const s = make()
  s.start()
  const kinds: string[] = []
  s.on('resync', (m) => kinds.push(m.kind))
  Fake.all[0].emit('open')
  Fake.all[0].emit('error')
  expect(Fake.all[0].closed).toBe(true)
  expect(s.connected).toBe(false)
  await vi.advanceTimersByTimeAsync(1100)
  expect(Fake.all).toHaveLength(2)
  Fake.all[1].emit('open')
  expect(kinds).toEqual(['reconnected'])
  s.stop()
  expect(Fake.all[1].closed).toBe(true)
})

test('a 401 stops reconnecting', async () => {
  const s = make(async () => {
    throw new ApiError(401, 'unauthorized', 'no')
  })
  s.start()
  Fake.all[0].emit('error')
  await vi.advanceTimersByTimeAsync(60000)
  expect(Fake.all).toHaveLength(1)
})
