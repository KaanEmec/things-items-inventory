import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { api } from './api'
import { conn } from './connection.svelte'
import { atLeast, auth, boot, can, login, requireLogin, takeReturnTo } from './auth.svelte'

type Handler = (url: string, init: RequestInit) => Response
function route(table: Record<string, Handler | Response>) {
  const f = vi.fn(async (url: string, init: RequestInit) => {
    const h = table[`${init.method} ${url}`]
    if (!h) throw new Error(`unmocked ${init.method} ${url}`)
    return typeof h === 'function' ? h(url, init) : h.clone()
  })
  vi.stubGlobal('fetch', f)
  return f
}
const unauth = () => Response.json({ error: { code: 'unauthorized', message: 'Sign in first.' } }, { status: 401 })
const me = {
  user: { id: 'u1', name: 'Sam', email: 's@x.io' },
  households: [{ id: 'h1', name: 'Home', role: 'admin' }, { id: 'h2', name: 'Cabin', role: 'viewer' }],
  active_household: 'h1',
  role: 'admin',
}

beforeEach(() => {
  auth.status = 'loading'
  auth.user = null
  auth.role = null
  auth.returnTo = ''
  vi.stubGlobal('location', { pathname: '/items', search: '?q=lamp' })
})
afterEach(() => vi.unstubAllGlobals())

test('setup needed leads to setup without asking for /me', async () => {
  const f = route({ 'GET /api/v1/setup': Response.json({ needed: true }) })
  await boot()
  expect(auth.status).toBe('setup')
  expect(f).toHaveBeenCalledTimes(1)
})

test('setup token requirement is remembered for the setup screen', async () => {
  route({ 'GET /api/v1/setup': Response.json({ needed: true, token_required: true }) })
  await boot()
  expect(auth.setupToken).toBe(true)
})

test('logged out: /me 401 shows login and remembers the page', async () => {
  route({ 'GET /api/v1/setup': Response.json({ needed: false }), 'GET /api/v1/me': unauth() })
  await boot()
  expect(auth.status).toBe('login')
  expect(auth.returnTo).toBe('/items?q=lamp')
})

test('logged in: loads user, households, active household and role', async () => {
  route({ 'GET /api/v1/setup': Response.json({ needed: false }), 'GET /api/v1/me': Response.json(me) })
  await boot()
  expect(auth.status).toBe('ready')
  expect(auth.user?.name).toBe('Sam')
  expect(auth.households).toHaveLength(2)
  expect(auth.householdId).toBe('h1')
  expect(auth.role).toBe('admin')
  expect(can('member')).toBe(true)
  expect(can('owner')).toBe(false)
})

test('login posts credentials then loads the session; wrong password is not a redirect', async () => {
  route({
    'POST /api/v1/auth/login': (_u, init) =>
      JSON.parse(init.body as string).password === 'right-password'
        ? Response.json({})
        : Response.json({ error: { code: 'invalid', message: 'Wrong email or password.' } }, { status: 401 }),
    'GET /api/v1/me': Response.json(me),
  })
  auth.status = 'login'
  await expect(login('s@x.io', 'nope')).rejects.toMatchObject({ status: 401, message: 'Wrong email or password.' })
  expect(auth.status).toBe('login')
  await login('s@x.io', 'right-password')
  expect(auth.status).toBe('ready')
})

test('a later 401 keeps the screen and flags the ended session', async () => {
  route({
    'GET /api/v1/setup': Response.json({ needed: false }),
    'GET /api/v1/me': Response.json(me),
    'GET /api/v1/items': unauth(),
  })
  await boot()
  expect(auth.status).toBe('ready')
  await api.get('/api/v1/items').catch(() => {})
  expect(auth.status).toBe('ready')
  expect(conn.sessionEnded).toBe(true)
  requireLogin('/items?q=lamp')
  expect(conn.sessionEnded).toBe(false)
  expect(auth.status).toBe('login')
  expect(auth.user).toBeNull()
  expect(takeReturnTo()).toBe('/items?q=lamp')
  expect(takeReturnTo()).toBe('/')
})

test('requireLogin does not remember the login page itself', () => {
  requireLogin('/login')
  expect(auth.returnTo).toBe('')
})

test('role ordering: viewer < member < admin < owner', () => {
  expect(atLeast('viewer', 'member')).toBe(false)
  expect(atLeast('member', 'member')).toBe(true)
  expect(atLeast('admin', 'member')).toBe(true)
  expect(atLeast('admin', 'owner')).toBe(false)
  expect(atLeast('owner', 'admin')).toBe(true)
  expect(atLeast(null, 'viewer')).toBe(false)
})
