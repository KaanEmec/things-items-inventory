// Reactive PWA state: service worker registration and update flow, install prompt, online state,
// and the one-time iOS guide. The worker is registered in production builds only.
import { currentEnv, installKind, shouldAutoShowIosGuide, type InstallKind } from './platform'
import { nextUpdate, type UpdateEvent, type UpdateState } from './update'

const DISMISS_KEY = 'pwa.iosGuideDismissed'

interface InstallPromptEvent extends Event {
  prompt(): Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>
}

function read(key: string): boolean {
  try {
    return localStorage.getItem(key) === '1'
  } catch {
    return false
  }
}
function write(key: string) {
  try {
    localStorage.setItem(key, '1')
  } catch {
    // Private mode: the guide may show again next visit.
  }
}

class Pwa {
  update = $state<UpdateState>('idle')
  online = $state(typeof navigator === 'undefined' || navigator.onLine !== false)
  installed = $state(false)
  private prompt = $state.raw<InstallPromptEvent | null>(null)
  iosGuideDismissed = $state(read(DISMISS_KEY))
  private waiting: ServiceWorker | null = null
  private started = false

  get kind(): InstallKind {
    return installKind(currentEnv(), this.prompt !== null)
  }
  /** One-time guide shown on its own (not from Settings). */
  get autoIosGuide() {
    return shouldAutoShowIosGuide(currentEnv(), this.iosGuideDismissed)
  }

  dismissIosGuide() {
    this.iosGuideDismissed = true
    write(DISMISS_KEY)
  }

  private send(e: UpdateEvent) {
    this.update = nextUpdate(this.update, e)
  }

  /** Call once from main.ts. */
  start() {
    if (this.started || typeof window === 'undefined') return
    this.started = true
    addEventListener('online', () => (this.online = true))
    addEventListener('offline', () => (this.online = false))
    addEventListener('beforeinstallprompt', (e) => {
      e.preventDefault()
      this.prompt = e as InstallPromptEvent
    })
    addEventListener('appinstalled', () => {
      this.prompt = null
      this.installed = true
    })
    if (import.meta.env.PROD && 'serviceWorker' in navigator) void this.register()
  }

  private async register() {
    const sw = navigator.serviceWorker
    let reloading = false
    sw.addEventListener('controllerchange', () => {
      // Only a person-requested update reloads; the first install also claims the page.
      if (this.update !== 'applying' || reloading) return
      reloading = true
      this.send('controlled')
      location.reload()
    })
    try {
      const reg = await sw.register('/sw.js', { scope: '/' })
      const watch = (w: ServiceWorker) => {
        this.waiting = w
        this.send('waiting')
      }
      if (reg.waiting && sw.controller) watch(reg.waiting)
      reg.addEventListener('updatefound', () => {
        const w = reg.installing
        w?.addEventListener('statechange', () => {
          if (w.state === 'installed' && sw.controller) watch(w)
        })
      })
      // Look for a new version when the app comes back to the front, and hourly.
      const check = () => void reg.update().catch(() => {})
      document.addEventListener('visibilitychange', () => document.visibilityState === 'visible' && check())
      setInterval(check, 60 * 60 * 1000)
    } catch {
      // No worker (insecure origin, blocked): the app works as a normal web page.
    }
  }

  /** "Reload": the waiting worker takes over, then the page reloads on controllerchange. */
  applyUpdate() {
    if (this.update !== 'available' || !this.waiting) return
    this.send('reload')
    this.waiting.postMessage({ type: 'SKIP_WAITING' })
    // If the new worker never takes over, offer the button again.
    setTimeout(() => this.update === 'applying' && this.send('failed'), 8000)
  }

  async install() {
    const p = this.prompt
    if (!p) return
    this.prompt = null // a prompt can be used once
    await p.prompt()
    await p.userChoice
  }
}

export const pwa = new Pwa()
