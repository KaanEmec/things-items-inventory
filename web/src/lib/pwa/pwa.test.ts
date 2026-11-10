import { expect, test } from 'vitest'
import { canShowIosGuide, installKind, isIos, isIosSafari, isStandalone, shouldAutoShowIosGuide, type Env } from './platform'
import { nextUpdate } from './update'

const iphoneSafari: Env = {
  userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1',
  platform: 'iPhone',
  maxTouchPoints: 5,
}
const iphoneChrome: Env = { ...iphoneSafari, userAgent: iphoneSafari.userAgent.replace('Version/18.0', 'CriOS/130.0') }
const ipadDesktopUa: Env = { userAgent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Version/18.0 Safari/605.1.15', platform: 'MacIntel', maxTouchPoints: 5 }
const mac: Env = { ...ipadDesktopUa, maxTouchPoints: 0 }
const android: Env = { userAgent: 'Mozilla/5.0 (Linux; Android 14; Pixel 8) Chrome/130.0 Mobile Safari/537.36', platform: 'Linux armv81', maxTouchPoints: 5 }

test('update: waits, asks, applies, reloads', () => {
  expect(nextUpdate('idle', 'waiting')).toBe('available')
  expect(nextUpdate('available', 'reload')).toBe('applying')
  expect(nextUpdate('applying', 'controlled')).toBe('idle')
})

test('update: nothing changes without a tap', () => {
  expect(nextUpdate('idle', 'reload')).toBe('idle')
  expect(nextUpdate('idle', 'controlled')).toBe('idle')
  expect(nextUpdate('available', 'controlled')).toBe('available')
  expect(nextUpdate('applying', 'waiting')).toBe('applying')
  expect(nextUpdate('available', 'waiting')).toBe('available')
})

test('update: a stuck apply can be retried', () => {
  expect(nextUpdate('applying', 'failed')).toBe('available')
})

test('iOS detection', () => {
  expect(isIos(iphoneSafari)).toBe(true)
  expect(isIos(ipadDesktopUa)).toBe(true)
  expect(isIos(mac)).toBe(false)
  expect(isIos(android)).toBe(false)
  expect(isIosSafari(iphoneSafari)).toBe(true)
  expect(isIosSafari(iphoneChrome)).toBe(false)
})

test('standalone detection', () => {
  expect(isStandalone({})).toBe(false)
  expect(isStandalone({ navStandalone: true })).toBe(true)
  expect(isStandalone({ displayStandalone: true })).toBe(true)
})

test('iOS guide: Safari, not installed, once', () => {
  expect(canShowIosGuide(iphoneSafari)).toBe(true)
  expect(canShowIosGuide({ ...iphoneSafari, navStandalone: true })).toBe(false)
  expect(canShowIosGuide(android)).toBe(false)
  expect(shouldAutoShowIosGuide(iphoneSafari, false)).toBe(true)
  expect(shouldAutoShowIosGuide(iphoneSafari, true)).toBe(false)
})

test('install kind', () => {
  expect(installKind({ ...android, displayStandalone: true }, false)).toBe('installed')
  expect(installKind(android, true)).toBe('prompt')
  expect(installKind(android, false)).toBe('none')
  expect(installKind(iphoneSafari, false)).toBe('ios')
  expect(installKind(mac, false)).toBe('none')
})
