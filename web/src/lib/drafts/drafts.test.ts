import { expect, test } from 'vitest'
import type { Item } from '../inventory/types'
import { warrantySourceText, answerFields, isGuess, isSure, queueState, readDraft } from './drafts'

const item = (o: Partial<Item>) => ({ ai_fields: [], ai_state: 'ready', ai_draft: null, ...o }) as Item

test('readDraft tolerates a missing draft', () => {
  expect(readDraft(item({}))).toEqual({ confidence: {}, needs: [], error: null, receipt_lines: [] })
})
test('isGuess only for untouched AI fields', () => {
  const d = { confidence: { name: 'guess', brand: 'sure' } }
  expect(isGuess(item({ ai_fields: ['name'], ai_draft: d }), 'name')).toBe(true)
  expect(isGuess(item({ ai_fields: [], ai_draft: d }), 'name')).toBe(false)
  expect(isGuess(item({ ai_fields: ['brand'], ai_draft: d }), 'brand')).toBe(false)
})
test('isSure needs ready, all sure, no questions', () => {
  const sure = { confidence: { name: 'sure' }, needs: [] }
  expect(isSure(item({ ai_fields: ['name'], ai_draft: sure }))).toBe(true)
  expect(isSure(item({ ai_fields: ['name'], ai_draft: { ...sure, needs: [{ kind: 'choose', question: 'q' }] } }))).toBe(false)
  expect(isSure(item({ ai_fields: ['name'], ai_draft: { confidence: { name: 'likely' } } }))).toBe(false)
  expect(isSure(item({ ai_state: 'reading', ai_draft: sure }))).toBe(false)
})
test('queueState and answerFields', () => {
  expect(queueState({ ai_state: 'queued' })).toBe('waiting')
  expect(queueState({ ai_state: null })).toBe('waiting')
  expect(queueState({ ai_state: 'failed' })).toBe('failed')
  expect(answerFields({ kind: 'choose', field: 'attributes.storage', question: '' }, '128 GB')).toEqual({ attributes: { storage: '128 GB' } })
  expect(answerFields({ kind: 'choose', field: 'model', question: '' }, 'X')).toEqual({ model: 'X' })
})
test('warrantySourceText', () => {
  expect(warrantySourceText('stated')).toContain('receipt')
  expect(warrantySourceText('default')).toContain('default')
  expect(warrantySourceText(null)).toBe('')
})
