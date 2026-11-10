// Minimal History-API router: current path as state, navigate(), and
// same-origin link click handling. The route table lives in routes/index.ts.
export const router = $state({
  path: typeof location === 'undefined' ? '/' : location.pathname,
  /** URL fragment without the `#`; one-time links keep their token here, away from server logs. */
  hash: typeof location === 'undefined' ? '' : location.hash.slice(1),
})

/** Token from a fragment like `token=abc` (or a bare `abc`). */
export function fragmentToken(hash: string): string {
  const h = hash.startsWith('token=') ? hash.slice(6) : hash
  try {
    return decodeURIComponent(h)
  } catch {
    return h
  }
}

export function navigate(to: string, replace = false) {
  history[replace ? 'replaceState' : 'pushState'](null, '', to)
  const u = new URL(to, 'http://x')
  router.path = u.pathname
  router.hash = u.hash.slice(1)
}

/** True when `href` is the current page or one of its parents (`/` matches only itself). */
export function isActive(href: string, path = router.path) {
  return href === '/' ? path === '/' : path === href || path.startsWith(href + '/')
}

/** Call once at startup; returns a cleanup function. */
export function startRouter() {
  const onPop = () => {
    router.path = location.pathname
    router.hash = location.hash.slice(1)
  }
  const onClick = (e: MouseEvent) => {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
    const a = (e.target as Element).closest('a')
    if (!a || a.target || a.hasAttribute('download') || a.origin !== location.origin) return
    e.preventDefault()
    navigate(a.pathname + a.search + a.hash)
  }
  addEventListener('popstate', onPop)
  addEventListener('hashchange', onPop)
  document.addEventListener('click', onClick)
  return () => {
    removeEventListener('popstate', onPop)
    removeEventListener('hashchange', onPop)
    document.removeEventListener('click', onClick)
  }
}
