import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { ApiError } from '../api'
import { buildForm } from './upload'
import { backoffMs, CaptureQueue, type QueueEntry } from './queue.svelte'
import { CaptureSession, groupIds } from './session.svelte'
import { MemoryStore } from './store'
import { ULID_RE, ulid } from './ulid'

const blob = () => new Blob(['x'], { type: 'image/jpeg' })
const flush = async () => {
  for (let i = 0; i < 20; i++) await Promise.resolve()
}
const entry = (o: Partial<QueueEntry>): QueueEntry => ({
  id: 'E' + Math.random().toString(36).slice(2),
  groupId: 'G1',
  role: 'item',
  placeId: null,
  blob: blob(),
  state: 'waiting',
  attempts: 0,
  error: null,
  seq: 0,
  nextAt: 0,
  ...o,
})

let clock = 0
let online = true
beforeEach(() => {
  clock = 1_000_000
  online = true
  vi.useFakeTimers()
})
afterEach(() => vi.useRealTimers())

function make(upload: (e: QueueEntry) => Promise<void>, store = new MemoryStore()) {
  const q = new CaptureQueue({ store, upload, isOnline: () => online, now: () => clock })
  return { q, store }
}

test('ulid has 26 Crockford characters and increases', () => {
  const a = ulid(1_700_000_000_000)
  const b = ulid(1_700_000_000_000)
  const c = ulid(1_700_000_000_001)
  for (const id of [a, b, c]) expect(id).toMatch(ULID_RE)
  expect(a < b && b < c).toBe(true)
  expect(ulid(1_700_000_000_000)).toMatch(ULID_RE) // clock stepping back still gives a valid id
})

test('groupIds: one shared id or one per photo', () => {
  let n = 0
  const make = () => 'id' + n++
  expect(groupIds(3, false, make)).toEqual(['id0', 'id0', 'id0'])
  expect(groupIds(3, true, make)).toEqual(['id1', 'id2', 'id3'])
})

test('Next item closes a group and starts a new one; an empty group stays', async () => {
  const { q } = make(() => new Promise(() => {}))
  await q.start()
  let n = 0
  const s = new CaptureSession(q, '', () => 'G' + n++)
  expect(s.nextItem()).toBe(false)
  await s.take('item', [blob()])
  await s.take('label', [blob()])
  expect(s.shots.map((e) => e.role)).toEqual(['item', 'label'])
  const first = s.groupId
  expect(s.nextItem()).toBe(true)
  expect(s.groupId).not.toBe(first)
  expect(s.shots).toEqual([])
  await s.take('item', [blob()])
  expect(q.entries.map((e) => e.groupId)).toEqual([first, first, s.groupId])
  expect(s.done).toBe(1)
})

test('contents photos need a container and carry its place', async () => {
  const { q } = make(() => new Promise(() => {}))
  await q.start()
  const s = new CaptureSession(q, '', () => 'G')
  await expect(s.take('contents', [blob()])).rejects.toThrow()
  s.setPlace({ id: 'P1' } as never)
  const [e] = await s.take('contents', [blob()])
  expect(e).toMatchObject({ role: 'contents', placeId: 'P1' })
})

test('retry with backoff: a failure waits, then succeeds; the blob is dropped from storage', async () => {
  let calls = 0
  const { q, store } = make(async () => {
    if (++calls < 3) throw new ApiError(503, 'unavailable', 'down')
  })
  await q.start()
  const e = await q.add({ groupId: 'G', role: 'item', placeId: null, blob: blob() })
  await flush()
  expect(calls).toBe(1)
  expect(q.get(e.id)).toMatchObject({ state: 'waiting', attempts: 1, nextAt: clock + backoffMs(1) })
  clock += 1000
  vi.advanceTimersByTime(1000)
  await flush()
  expect(calls).toBe(2)
  expect(q.get(e.id)?.nextAt).toBe(clock + 2000)
  clock += 2000
  vi.advanceTimersByTime(2000)
  await flush()
  expect(calls).toBe(3)
  expect(q.get(e.id)).toMatchObject({ state: 'uploaded' })
  expect(store.rows.size).toBe(0)
  expect(backoffMs(20)).toBe(30_000)
})

test('after the last attempt, or on a 4xx, the photo fails and Retry restarts it', async () => {
  let fail = true
  const { q } = make(async () => {
    if (fail) throw new ApiError(415, 'unsupported_type', 'Only JPEG')
  })
  await q.start()
  const e = await q.add({ groupId: 'G', role: 'item', placeId: null, blob: blob() })
  await flush()
  expect(q.get(e.id)).toMatchObject({ state: 'failed', error: 'Only JPEG' })
  fail = false
  await q.retry(e.id)
  await flush()
  expect(q.get(e.id)?.state).toBe('uploaded')
})

test('resume after reload: entries left uploading are sent again, uploaded leftovers are dropped', async () => {
  const store = new MemoryStore()
  await store.put(entry({ id: 'A', state: 'uploading', attempts: 1, seq: 1, groupId: 'G1' }))
  await store.put(entry({ id: 'B', state: 'waiting', seq: 2, groupId: 'G2' }))
  await store.put(entry({ id: 'C', state: 'uploaded', blob: null, seq: 3, groupId: 'G3' }))
  const sent: string[] = []
  const { q } = make(async (e) => void sent.push(e.id), store)
  await q.start()
  await flush()
  expect(sent.sort()).toEqual(['A', 'B'])
  expect(store.rows.size).toBe(0)
  expect(q.pending).toBe(0)
})

test('retry while the same photo is in flight does not upload it twice', async () => {
  let calls = 0
  let finish!: () => void
  const { q } = make(
    () =>
      new Promise<void>((r) => {
        calls++
        finish = r
      }),
  )
  await q.start()
  const e = await q.add({ groupId: 'G', role: 'item', placeId: null, blob: blob() })
  await flush()
  expect(q.get(e.id)?.state).toBe('uploading')
  await q.retry(e.id)
  await q.retryAll()
  expect(await q.remove(e.id)).toBe(false)
  await flush()
  expect(calls).toBe(1)
  finish()
  await flush()
  expect(q.get(e.id)?.state).toBe('uploaded')
  expect(calls).toBe(1)
})

test('two at a time, and photos of one item go one after another', async () => {
  const running: string[] = []
  let max = 0
  const done: Array<() => void> = []
  const { q } = make(
    (e) =>
      new Promise<void>((r) => {
        running.push(e.id)
        max = Math.max(max, running.length)
        done.push(() => (running.splice(running.indexOf(e.id), 1), r()))
      }),
  )
  await q.start()
  await q.add({ groupId: 'A', role: 'item', placeId: null, blob: blob() })
  await q.add({ groupId: 'A', role: 'label', placeId: null, blob: blob() })
  await q.add({ groupId: 'B', role: 'item', placeId: null, blob: blob() })
  await q.add({ groupId: 'C', role: 'item', placeId: null, blob: blob() })
  await flush()
  expect(running.length).toBe(2) // A.item and B.item; A.label waits for A.item
  done.shift()!()
  await flush()
  expect(max).toBe(2)
  expect(q.entries.filter((e) => e.state === 'uploading').length).toBe(2)
})

test('offline: nothing is sent and no try is used; back online it uploads', async () => {
  online = false
  let calls = 0
  const { q } = make(async () => void calls++)
  await q.start()
  const e = await q.add({ groupId: 'G', role: 'item', placeId: null, blob: blob() })
  await flush()
  expect(calls).toBe(0)
  expect(q.pending).toBe(1)
  online = true
  q.setOnline(true)
  await flush()
  expect(calls).toBe(1)
  expect(q.get(e.id)?.state).toBe('uploaded')
})

test('a network error while offline waits for the online event without using a try', async () => {
  let calls = 0
  const { q } = make(async () => {
    if (++calls === 1) {
      online = false
      throw new ApiError(0, 'network', 'offline')
    }
  })
  await q.start()
  const e = await q.add({ groupId: 'G', role: 'item', placeId: null, blob: blob() })
  await flush()
  expect(q.get(e.id)).toMatchObject({ state: 'waiting', attempts: 0 })
  online = true
  q.setOnline(true)
  await flush()
  expect(q.get(e.id)?.state).toBe('uploaded')
})

test('a contents photo is sent to the place alone, with no item id', () => {
  const f = buildForm(entry({ role: 'contents', placeId: 'P1', groupId: 'G9' }))
  expect(f.get('place_id')).toBe('P1')
  expect(f.get('item_id')).toBeNull()
  expect(f.get('role')).toBe('contents')
})
