import { afterEach, expect, test, vi } from 'vitest'
import { ApiError, api } from './api'

function mockFetch(res: Response | Error) {
  const f = vi.fn(async () => {
    if (res instanceof Error) throw res
    return res
  })
  vi.stubGlobal('fetch', f)
  return f
}

afterEach(() => vi.unstubAllGlobals())

test('get returns parsed JSON and sends same-origin credentials', async () => {
  const f = mockFetch(Response.json({ version: 'v1' }))
  expect(await api.get('/api/health')).toEqual({ version: 'v1' })
  expect(f).toHaveBeenCalledWith('/api/health', expect.objectContaining({ method: 'GET', credentials: 'same-origin' }))
})

test('post sends a JSON body', async () => {
  const f = mockFetch(Response.json({ id: 1 }, { status: 201 }))
  await api.post('/api/v1/items', { name: 'lamp' })
  const init = (f.mock.calls[0] as any)[1]
  expect(init.method).toBe('POST')
  expect(init.body).toBe('{"name":"lamp"}')
  expect(init.headers['Content-Type']).toBe('application/json')
})

test('del tolerates an empty response', async () => {
  mockFetch(new Response(null, { status: 204 }))
  expect(await api.del('/api/v1/items/1')).toBeUndefined()
})

test('error shape becomes ApiError', async () => {
  mockFetch(Response.json({ error: { code: 'bad_request', message: 'Fix it.', fields: { name: 'Required.' } } }, { status: 400 }))
  const err = await api.post('/api/v1/items', {}).catch((e) => e)
  expect(err).toBeInstanceOf(ApiError)
  expect(err).toMatchObject({ status: 400, code: 'bad_request', message: 'Fix it.', fields: { name: 'Required.' } })
})

test('non-JSON error response gets a sensible error', async () => {
  mockFetch(new Response('<html>oops</html>', { status: 502 }))
  const err: any = await api.get('/api/v1/x').catch((e) => e)
  expect(err).toMatchObject({ status: 502, code: 'unknown', fields: {} })
  expect(err.message).toContain('502')
})

test('non-JSON success response is an error', async () => {
  mockFetch(new Response('hello', { status: 200 }))
  expect(await api.get('/api/v1/x').catch((e) => e)).toMatchObject({ code: 'bad_response' })
})

test('network failure becomes ApiError with status 0', async () => {
  mockFetch(new TypeError('failed to fetch'))
  expect(await api.get('/api/v1/x').catch((e) => e)).toMatchObject({ status: 0, code: 'network' })
})
