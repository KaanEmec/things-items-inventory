// Install and platform detection, pure so it can be tested. iOS has no install prompt: Safari
// needs the Share → Add to Home Screen steps explained.

export interface Env {
  userAgent: string
  platform: string
  maxTouchPoints: number
  /** navigator.standalone (iOS only). */
  navStandalone?: boolean
  /** matchMedia('(display-mode: standalone)').matches */
  displayStandalone?: boolean
}

export function isIos(e: Pick<Env, 'userAgent' | 'platform' | 'maxTouchPoints'>): boolean {
  if (/iPhone|iPad|iPod/.test(e.userAgent)) return true
  // iPadOS 13+ reports itself as a Mac with a touch screen.
  return e.platform === 'MacIntel' && e.maxTouchPoints > 1
}

/** Safari proper: not Chrome, Firefox, Edge, Opera or another iOS browser shell. */
export function isIosSafari(e: Pick<Env, 'userAgent' | 'platform' | 'maxTouchPoints'>): boolean {
  return isIos(e) && !/CriOS|FxiOS|EdgiOS|OPiOS|DuckDuckGo/.test(e.userAgent)
}

export function isStandalone(e: Pick<Env, 'navStandalone' | 'displayStandalone'>): boolean {
  return e.navStandalone === true || e.displayStandalone === true
}

/** The iOS guide is offered in Safari when the app is not yet on the Home Screen. */
export function canShowIosGuide(e: Env): boolean {
  return isIosSafari(e) && !isStandalone(e)
}

/** The automatic, one-time guide: only when never dismissed. */
export function shouldAutoShowIosGuide(e: Env, dismissed: boolean): boolean {
  return !dismissed && canShowIosGuide(e)
}

export type InstallKind = 'installed' | 'prompt' | 'ios' | 'none'

/** What the Settings → App section offers. */
export function installKind(e: Env, hasPrompt: boolean): InstallKind {
  if (isStandalone(e)) return 'installed'
  if (hasPrompt) return 'prompt'
  if (canShowIosGuide(e)) return 'ios'
  return 'none'
}

export function currentEnv(): Env {
  const nav = navigator as Navigator & { standalone?: boolean }
  return {
    userAgent: nav.userAgent,
    platform: nav.platform,
    maxTouchPoints: nav.maxTouchPoints ?? 0,
    navStandalone: nav.standalone,
    displayStandalone: typeof matchMedia === 'function' && matchMedia('(display-mode: standalone)').matches,
  }
}
