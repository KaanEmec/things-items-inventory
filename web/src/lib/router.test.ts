import { expect, test } from 'vitest'
import { isActive } from './router.svelte'

test('isActive matches exact root and path prefixes', () => {
  expect(isActive('/', '/')).toBe(true)
  expect(isActive('/', '/items')).toBe(false)
  expect(isActive('/items', '/items/5')).toBe(true)
  expect(isActive('/items', '/itemsX')).toBe(false)
})
