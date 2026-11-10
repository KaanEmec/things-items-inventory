import { expect, test } from 'vitest'
import { APP_NAME } from './app'

test('app has a name', () => {
  expect(APP_NAME).not.toBe('')
})
