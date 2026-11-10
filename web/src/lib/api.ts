// Tiny typed client for the JSON API. Every failure becomes an ApiError.

export class ApiError extends Error {
  constructor(
    public status: number, // 0 when the server could not be reached
    public code: string,
    message: string,
    public fields: Record<string, string> = {},
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

export type Page<T> = { items: T[]; next_cursor: string }

let onUnauthorized: ((path: string) => void) | undefined
/** The auth store registers here; called when a signed-in request comes back 401. */
export function setUnauthorizedHandler(fn: (path: string) => void) {
  onUnauthorized = fn
}
// Public endpoints answer 401 for wrong credentials, which is not an expired session.
const publicPath = /^\/api\/v1\/(auth\/(login|reset)|setup|invites\/|me\/password)/

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  let res: Response
  try {
    res = await fetch(path, {
      method,
      credentials: 'same-origin',
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch {
    throw new ApiError(0, 'network', 'Could not reach the server. Check your connection.')
  }

  let data: any = undefined
  const text = await res.text()
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      if (res.ok) throw new ApiError(res.status, 'bad_response', 'The server sent an unexpected response.')
    }
  }

  if (res.status === 401 && !publicPath.test(path)) onUnauthorized?.(path)
  if (!res.ok) {
    const e = data?.error
    throw new ApiError(
      res.status,
      typeof e?.code === 'string' ? e.code : 'unknown',
      typeof e?.message === 'string' ? e.message : `Request failed (${res.status}).`,
      e?.fields ?? {},
    )
  }
  return data as T
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body ?? {}),
  put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body ?? {}),
  patch: <T>(path: string, body?: unknown) => request<T>('PATCH', path, body ?? {}),
  del: <T = void>(path: string) => request<T>('DELETE', path),
}
