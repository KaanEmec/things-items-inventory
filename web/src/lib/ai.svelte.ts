// AI profile state, shared by Settings and every later AI control.
// `ai.status('vision')` tells whether AI features can be used right now.
import { api } from './api'

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
  effective,
  status,
}
