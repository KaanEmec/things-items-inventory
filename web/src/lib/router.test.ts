import { expect, test } from 'vitest'
import { isActive } from './router.svelte'

test('isActive matches exact root and path prefixes', () => {
  expect(isActive('/', '/')).toBe(true)
  expect(isActive('/', '/items')).toBe(false)
  expect(isActive('/items', '/items/5')).toBe(true)
  expect(isActive('/items', '/itemsX')).toBe(false)
})

test('fragmentToken reads token=... or a bare fragment', async () => {
  const { fragmentToken } = await import('./router.svelte')
  expect(fragmentToken('token=abc_DEF-1')).toBe('abc_DEF-1')
  expect(fragmentToken('abc')).toBe('abc')
  expect(fragmentToken('')).toBe('')
})
