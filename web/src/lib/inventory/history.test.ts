import { expect, test } from 'vitest'
import { actorLabel, sentence, type HistoryEntry } from './history'

const user = { kind: 'user', user_id: 'u1', name: 'Emma Stone', initials: 'ES', token_name: null } as const
const entry = (kind: string, summary: Record<string, unknown> = {}, actor: HistoryEntry['actor'] = user): HistoryEntry => ({
  id: 'e1', at: '2026-11-07T10:00:00Z', kind, actor, item_id: 'i1', item_name: 'Drill', place_id: null, place_name: null,
  summary: { subject: 'item', ...summary },
})

test('one sentence per event kind', () => {
  const s = (kind: string, summary?: Record<string, unknown>) => sentence(entry(kind, summary))
  expect(s('created', { name: 'Drill' })).toBe('Emma added it')
  expect(s('created', { subject: 'lines', count: 4 })).toBe('Emma added 4 items')
  expect(s('edited', { fields: ['brand', 'model'] })).toBe('Emma changed brand and model')
  expect(s('edited', { fields: ['a', 'b', 'c', 'd', 'e'] })).toBe('Emma changed a, b and c and 2 more')
  expect(s('edited', { fields: ['purchase.price_minor'] })).toBe('Emma changed purchase price minor')
  expect(s('moved', { from: 'drawer · bedroom', to: 'Box 12 · garage' })).toBe('Emma moved it to Box 12')
  expect(s('moved', { from: 'drawer', to: null })).toBe('Emma took it out of its place')
  expect(s('status_changed', { from: 'in_use', to: 'to_sell' })).toBe('Emma set the status to to sell / donate')
  expect(s('photo_added')).toBe('Emma added a photo')
  expect(s('confirmed')).toBe('Emma confirmed the details')
  expect(s('seen')).toBe('Emma saw it here')
  expect(s('packed')).toBe('Emma packed it')
  expect(s('opened', { subject: 'box' })).toBe('Emma opened the box')
  expect(s('arrived', { subject: 'box' })).toBe('Emma marked the box as arrived')
  expect(s('unpacked')).toBe('Emma unpacked it')
  expect(s('deleted')).toBe('Emma deleted it')
  expect(s('restored')).toBe('Emma restored it')
  expect(s('settings_changed')).toBe('Emma changed the settings')
  expect(s('something_new')).toBe('Emma something new')
})

test('actors: you, AI and access tokens', () => {
  expect(actorLabel(user, 'u1')).toBe('You')
  expect(actorLabel(user, 'u2')).toBe('Emma')
  const ai = { kind: 'ai', user_id: null, name: 'AI', initials: 'AI', token_name: null } as const
  expect(sentence(entry('edited', { fields: ['brand', 'model'] }, ai))).toBe('AI filled brand and model')
  const tok = { ...user, kind: 'token', token_name: 'Claude agent' } as const
  expect(sentence(entry('moved', { to: 'shelf · office' }, tok))).toBe('Emma moved it to shelf · via Claude agent')
})
