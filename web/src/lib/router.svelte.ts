// Minimal History-API router: current path as state, navigate(), and
// same-origin link click handling. The route table lives in routes/index.ts.
export const router = $state({ path: typeof location === 'undefined' ? '/' : location.pathname })

export function navigate(to: string, replace = false) {
  history[replace ? 'replaceState' : 'pushState'](null, '', to)
  router.path = new URL(to, 'http://x').pathname
}

/** True when `href` is the current page or one of its parents (`/` matches only itself). */
export function isActive(href: string, path = router.path) {
  return href === '/' ? path === '/' : path === href || path.startsWith(href + '/')
}

/** Call once at startup; returns a cleanup function. */
export function startRouter() {
  const onPop = () => (router.path = location.pathname)
  const onClick = (e: MouseEvent) => {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
    const a = (e.target as Element).closest('a')
    if (!a || a.target || a.hasAttribute('download') || a.origin !== location.origin) return
    e.preventDefault()
    navigate(a.pathname + a.search + a.hash)
  }
  addEventListener('popstate', onPop)
  document.addEventListener('click', onClick)
  return () => {
    removeEventListener('popstate', onPop)
    document.removeEventListener('click', onClick)
  }
}
