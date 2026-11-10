// AI profile state, shared by Settings and every later AI control.
// `ai.status('vision')` tells whether AI features can be used right now.
import { api, type Page } from './api'

export type AIRole = 'vision' | 'text'
export type AIStatus = 'working' | 'not_set' | 'failing' | 'paused'

export interface AIProfile {
  role: AIRole
  source: 'household' | 'server'
  base_url: string
  model: string
  token_set: boolean
  token_hint: string
  same_as_vision?: boolean
  output_mode?: string
  test_result?: { ok?: boolean | null } | null
  tested_at: string | null
  failing_since: string | null
  updated_at: string | null
}

const data = $state({
  vision: null as AIProfile | null,
  text: null as AIProfile | null,
  paused: false,
  loaded: false,
})

/** Loads both profiles and the household's pause flag. */
async function load() {
  const [p, h] = await Promise.all([
    api.get<{ vision: AIProfile; text: AIProfile }>('/api/v1/settings/ai'),
    api.get<{ ai_paused?: boolean }>('/api/v1/household'),
  ])
  data.vision = p.vision
  data.text = p.text
  data.paused = !!h.ai_paused
  data.loaded = true
}

let loading: Promise<void> | null = null
/** Loads once; later callers share the result. AI controls call this so no screen has to. Never rejects. */
function ensure(): Promise<void> {
  if (data.loaded) return Promise.resolve()
  loading ??= load().then(
    () => void (loading = null),
    () => void (loading = null),
  )
  return loading
}

/** Admin only: pauses or resumes AI for the household. Queued work waits and is not lost. */
async function setPaused(paused: boolean) {
  await api.patch('/api/v1/household', { ai_paused: paused })
  data.paused = paused
}

const configured = (p: AIProfile | null) => !!p && p.token_set && !!p.model

/** The profile that actually does the work for a role ("text" may follow "vision"). */
function effective(role: AIRole): AIProfile | null {
  const p = data[role]
  return role === 'text' && p?.same_as_vision ? data.vision : p
}

function status(role: AIRole): AIStatus {
  if (data.paused) return 'paused'
  const p = effective(role)
  if (!configured(p)) return 'not_set'
  return p!.failing_since ? 'failing' : 'working'
}

export const ai = {
  get vision() { return data.vision },
  get text() { return data.text },
  get paused() { return data.paused },
  get loaded() { return data.loaded },
  load,
  ensure,
  setPaused,
  effective,
  status,
}

export interface AIActivity {
  id: string
  at: string
  feature: string
  role: AIRole
  model: string
  tokens_in: number | null
  tokens_out: number | null
  cost: number | null
  duration_ms: number
  ok: boolean
  error_code: string | null
  error: string | null
  job_id: string | null
  item_id: string | null
}
export interface AIUsage {
  requests: number
  failures: number
  tokens_in: number
  tokens_out: number
  cost: number
}

export const listActivity = (cursor?: string | null, limit = 20) =>
  api.get<Page<AIActivity>>('/api/v1/settings/ai/activity?limit=' + limit + (cursor ? '&cursor=' + encodeURIComponent(cursor) : ''))
export const getUsage = () => api.get<{ month: string; roles: Record<AIRole, AIUsage> }>('/api/v1/settings/ai/usage')

const features: Record<string, string> = {
  item_draft: 'Item draft',
  label: 'Label reading',
  receipt: 'Receipt reading',
  contents: 'Contents photo',
  row_completion: 'Row completion',
  text_to_items: 'Text to items',
  csv_mapping: 'CSV mapping',
  test: 'Connection test',
}
/** Plain words for an activity feature key. */
export function featureLabel(key: string): string {
  if (features[key]) return features[key]
  const s = key.replace(/[_-]/g, ' ')
  return s.charAt(0).toUpperCase() + s.slice(1)
}

/** Empty when the endpoint reported no cost. */
export const formatCost = (c: number | null | undefined) => (c == null || c <= 0 ? '' : c < 0.01 ? 'under $0.01' : '$' + c.toFixed(2))
export const formatTokens = (n: number | null | undefined) =>
  n == null ? '' : n >= 10000 ? Math.round(n / 1000) + 'k' : n >= 1000 ? (n / 1000).toFixed(1) + 'k' : String(n)
