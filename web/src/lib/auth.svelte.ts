// Session state: who is signed in, in which household, with which role.
// App.svelte decides what to show from `auth.status`.
import { ApiError, api, setUnauthorizedHandler } from './api'

export type Role = 'viewer' | 'member' | 'admin' | 'owner'
export const roles: Role[] = ['viewer', 'member', 'admin', 'owner']
export const roleLabels: Record<Role, string> = { viewer: 'Viewer', member: 'Member', admin: 'Admin', owner: 'Owner' }

export interface User {
  id: string
  name: string
  email: string
}
export interface HouseholdRef {
  id: string
  name: string
  role: Role
}

const V1 = '/api/v1'

export const auth = $state({
  /** loading: not known yet; setup: no user exists; login: signed out; ready: signed in; error: server unreachable. */
  status: 'loading' as 'loading' | 'setup' | 'login' | 'ready' | 'error',
  user: null as User | null,
  households: [] as HouseholdRef[],
  householdId: '',
  role: null as Role | null,
  /** Page to return to after signing in. */
  returnTo: '',
  error: '',
})

/** True when `have` is at least `need` (viewer < member < admin < owner). */
export function atLeast(have: Role | null | undefined, need: Role): boolean {
  return have != null && roles.indexOf(have) >= roles.indexOf(need)
}

/** True when the signed-in person's role in the active household is at least `need`. */
export function can(need: Role): boolean {
  return atLeast(auth.role, need)
}

export const activeHousehold = () => auth.households.find((h) => h.id === auth.householdId)

function here() {
  return typeof location === 'undefined' ? '/' : location.pathname + location.search
}

function clearSession() {
  auth.user = null
  auth.households = []
  auth.householdId = ''
  auth.role = null
}

// GET /me: { user, households: [{id, name, role}], active_household: id | {id, ...}, role }
function applyMe(me: any) {
  const households: HouseholdRef[] = (me.households ?? []).map((h: any) => ({
    id: String(h.id ?? h.household_id),
    name: h.name ?? h.household_name ?? '',
    role: h.role,
  }))
  const a = me.active_household_id ?? me.active_household
  const id = String(typeof a === 'object' && a ? a.id : (a ?? households[0]?.id ?? ''))
  auth.user = me.user
  auth.households = households
  auth.householdId = id
  auth.role = me.role ?? households.find((h) => h.id === id)?.role ?? null
  auth.status = 'ready'
}

/** Sends the person to Login and remembers where they were. */
export function requireLogin(returnTo = here()) {
  if (auth.status === 'ready') clearSession()
  if (returnTo !== '/login' && returnTo !== '/setup') auth.returnTo = returnTo
  auth.status = 'login'
}

setUnauthorizedHandler(() => requireLogin())

/** Loads /me. Returns false (and shows Login) when there is no session. */
export async function loadSession(): Promise<boolean> {
  try {
    applyMe(await api.get(`${V1}/me`))
    return true
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) {
      requireLogin()
      return false
    }
    throw e
  }
}

/** App start: is setup needed, is there a session. */
export async function boot() {
  auth.status = 'loading'
  auth.error = ''
  try {
    const s = await api.get<{ needed: boolean }>(`${V1}/setup`)
    if (s.needed) {
      auth.status = 'setup'
      return
    }
    await loadSession()
  } catch (e) {
    auth.error = e instanceof Error ? e.message : 'Something went wrong.'
    auth.status = 'error'
  }
}

export async function login(email: string, password: string) {
  await api.post(`${V1}/auth/login`, { email, password })
  await loadSession()
}

export async function logout() {
  await api.post(`${V1}/auth/logout`)
  auth.returnTo = ''
  clearSession()
  auth.status = 'login'
}

export async function logoutAll() {
  await api.post(`${V1}/auth/logout-all`)
  auth.returnTo = ''
  clearSession()
  auth.status = 'login'
}

export async function switchHousehold(id: string) {
  await api.put(`${V1}/me/household`, { household_id: id })
  await loadSession()
}

/** Takes the stored return path (once). */
export function takeReturnTo(): string {
  const to = auth.returnTo || '/'
  auth.returnTo = ''
  return to
}
