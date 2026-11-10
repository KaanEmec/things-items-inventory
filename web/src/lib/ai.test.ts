import { afterEach, expect, test, vi } from 'vitest'
import { ai } from './ai.svelte'

const profile = (role: string, o: object = {}) => ({
  role, source: 'household', base_url: 'https://x/v1', model: 'm', token_set: true, token_hint: 'a1b2',
  tested_at: null, failing_since: null, updated_at: null, ...o,
})

function serve(p: object, household: object = {}) {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => Response.json(url.endsWith('/household') ? household : p)))
}
afterEach(() => vi.unstubAllGlobals())

test('working, not set, failing and paused', async () => {
  serve({ vision: profile('vision'), text: profile('text', { token_set: false }) })
  await ai.load()
  expect(ai.status('vision')).toBe('working')
  expect(ai.status('text')).toBe('not_set')

  serve({ vision: profile('vision', { failing_since: '2026-11-01T10:00:00Z' }), text: profile('text') })
  await ai.load()
  expect(ai.status('vision')).toBe('failing')

  serve({ vision: profile('vision'), text: profile('text') }, { ai_paused: true })
  await ai.load()
  expect(ai.status('vision')).toBe('paused')
})

test('text follows vision when same_as_vision is on', async () => {
  serve({
    vision: profile('vision', { failing_since: '2026-11-01T10:00:00Z' }),
    text: profile('text', { same_as_vision: true, token_set: false, model: '' }),
  })
  await ai.load()
  expect(ai.status('text')).toBe('failing')
})
