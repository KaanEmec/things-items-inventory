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

test('paused wins over everything, including a missing profile', async () => {
  serve({ vision: profile('vision', { token_set: false }), text: profile('text') }, { ai_paused: true })
  await ai.load()
  expect(ai.status('vision')).toBe('paused')
  expect(ai.status('text')).toBe('paused')
})

test('setPaused patches the household and flips the status', async () => {
  serve({ vision: profile('vision'), text: profile('text') })
  await ai.load()
  const f = vi.fn(async () => Response.json({}))
  vi.stubGlobal('fetch', f)
  await ai.setPaused(true)
  expect(f).toHaveBeenCalledWith('/api/v1/household', expect.objectContaining({ method: 'PATCH', body: '{"ai_paused":true}' }))
  expect(ai.status('vision')).toBe('paused')
  await ai.setPaused(false)
  expect(ai.status('vision')).toBe('working')
})

test('activity words and numbers', async () => {
  const { featureLabel, formatCost, formatTokens } = await import('./ai.svelte')
  expect(featureLabel('item_draft')).toBe('Item draft')
  expect(featureLabel('some_new_thing')).toBe('Some new thing')
  expect(formatCost(null)).toBe('')
  expect(formatCost(0.1234)).toBe('$0.12')
  expect(formatTokens(950)).toBe('950')
  expect(formatTokens(1500)).toBe('1.5k')
})
