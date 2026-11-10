import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../api'
import { createSuggest } from './suggest'
import type { SuggestRow } from './sheet.svelte'

const row = (id: string, name: string, values: Record<string, string> = {}): SuggestRow => ({ id, name, values })
const answer = (ids: string[], cells: Record<string, string> = { brand: 'Sony' }) => ({ rows: ids.map((id) => ({ id, cells })) })

beforeEach(() => vi.useFakeTimers())
afterEach(() => vi.useRealTimers())

describe('suggest', () => {
  it('batches rows asked at the same moment into one request', async () => {
    const post = vi.fn(async (rows: { id: string }[]) => answer(rows.map((r) => r.id)))
    const suggest = createSuggest({ post, working: () => true, batchMs: 100 })
    const a = suggest([row('1', 'wh1000xm4'), row('2', 'cable')])
    await vi.advanceTimersByTimeAsync(40)
    const b = suggest([row('3', 'hub')])
    await vi.advanceTimersByTimeAsync(100)
    expect(post).toHaveBeenCalledTimes(1)
    expect(post.mock.calls[0][0].map((r) => r.id)).toEqual(['1', '2', '3'])
    expect((await a).map((r) => r.id)).toEqual(['1', '2'])
    expect((await b).map((r) => r.id)).toEqual(['3'])
  })

  it('splits more than 20 rows into several requests', async () => {
    const post = vi.fn(async (rows: { id: string }[]) => answer(rows.map((r) => r.id)))
    const suggest = createSuggest({ post, working: () => true })
    const p = suggest(Array.from({ length: 25 }, (_, i) => row(String(i), 'thing ' + i)))
    await vi.advanceTimersByTimeAsync(200)
    expect((await p).length).toBe(25)
    expect(post.mock.calls.map((c) => c[0].length)).toEqual([20, 5])
  })

  it('sends only the name and a picked category', async () => {
    const post = vi.fn(async (_rows: unknown[]) => answer([]))
    const suggest = createSuggest({ post, working: () => true })
    void suggest([row('1', ' lamp ', { category: 'other', brand: 'x', place: 'p' }), row('2', 'drill', {})])
    await vi.advanceTimersByTimeAsync(200)
    expect(post.mock.calls[0][0]).toEqual([
      { id: '1', name: 'lamp', values: { category: 'other' } },
      { id: '2', name: 'drill', values: {} },
    ])
  })

  it('drops an answer when the row was asked again with another name meanwhile', async () => {
    let release!: () => void
    const post = vi.fn(
      (rows: { id: string }[]) => new Promise<ReturnType<typeof answer>>((r) => (release = () => r(answer(rows.map((x) => x.id), { brand: 'Old' })))),
    )
    const suggest = createSuggest({ post, working: () => true, batchMs: 10 })
    const first = suggest([row('1', 'wh1000')])
    await vi.advanceTimersByTimeAsync(20) // first request is in flight
    void suggest([row('1', 'something else')])
    release()
    expect(await first).toEqual([])
  })

  it('leaves out rows the server had nothing for', async () => {
    const post = vi.fn(async () => answer(['1']))
    const suggest = createSuggest({ post, working: () => true })
    const p = suggest([row('1', 'a'), row('2', 'asdf')])
    await vi.advanceTimersByTimeAsync(200)
    expect((await p).map((r) => r.id)).toEqual(['1'])
  })

  it('stops after repeated failures with one notice, and stays quiet until then', async () => {
    const post = vi.fn(async () => {
      throw new ApiError(502, 'ai_failed', 'x')
    })
    const notify = vi.fn()
    const suggest = createSuggest({ post, working: () => true, notify, maxFailures: 3 })
    for (let i = 0; i < 3; i++) {
      const p = suggest([row('1', 'name ' + i)])
      await vi.advanceTimersByTimeAsync(200)
      expect(await p).toEqual([])
    }
    expect(notify).toHaveBeenCalledTimes(1)
    expect(await suggest([row('1', 'more')])).toEqual([])
    await vi.advanceTimersByTimeAsync(200)
    expect(post).toHaveBeenCalledTimes(3) // no more requests this session
    expect(notify).toHaveBeenCalledTimes(1)
  })

  it('a success resets the failure count', async () => {
    let fail = true
    const post = vi.fn(async (rows: { id: string }[]) => {
      if (fail) throw new ApiError(0, 'network', 'x')
      return answer(rows.map((r) => r.id))
    })
    const notify = vi.fn()
    const suggest = createSuggest({ post, working: () => true, notify, maxFailures: 3 })
    const run = async (n: string) => {
      const p = suggest([row('1', n)])
      await vi.advanceTimersByTimeAsync(200)
      return p
    }
    await run('a')
    await run('b')
    fail = false
    expect((await run('c')).length).toBe(1)
    fail = true
    await run('d')
    await run('e')
    expect(notify).not.toHaveBeenCalled()
  })

  it('stops quietly when the server says AI is not set up or paused', async () => {
    const post = vi.fn(async () => {
      throw new ApiError(409, 'ai_paused', 'paused')
    })
    const notify = vi.fn()
    const suggest = createSuggest({ post, working: () => true, notify })
    const p = suggest([row('1', 'a')])
    await vi.advanceTimersByTimeAsync(200)
    expect(await p).toEqual([])
    expect(await suggest([row('1', 'b')])).toEqual([])
    expect(post).toHaveBeenCalledTimes(1)
    expect(notify).not.toHaveBeenCalled()
  })

  it('backs off after too many requests without counting a failure', async () => {
    const post = vi.fn(async () => {
      throw new ApiError(429, 'rate_limited', 'slow')
    })
    const notify = vi.fn()
    const suggest = createSuggest({ post, working: () => true, notify, rateLimitMs: 1000, maxFailures: 1 })
    const p = suggest([row('1', 'a')])
    await vi.advanceTimersByTimeAsync(200)
    await p
    expect(await suggest([row('1', 'b')])).toEqual([])
    expect(post).toHaveBeenCalledTimes(1)
    expect(notify).not.toHaveBeenCalled()
  })

  it('returns nothing and asks nothing when AI does not work', async () => {
    const post = vi.fn(async () => answer(['1']))
    const notify = vi.fn()
    let on = false
    const suggest = createSuggest({ post, working: () => on, notify })
    expect(await suggest([row('1', 'wh1000xm4')])).toEqual([])
    expect(post).not.toHaveBeenCalled()
    expect(notify).not.toHaveBeenCalled()
    on = true // AI set up later in the session
    const p = suggest([row('1', 'wh1000xm4')])
    await vi.advanceTimersByTimeAsync(200)
    expect((await p).length).toBe(1)
  })
})
