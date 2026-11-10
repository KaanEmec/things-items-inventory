// J7.5: CSV import. Each step sends the file again (the server keeps no upload); the mapping goes along as JSON.
import { api } from '../api'

export interface Column {
  target: string
  attribute?: string
  confidence?: string
}
export interface Mapping {
  has_header: boolean
  delimiter: string
  columns: Column[]
  category: string
  date_format: string
  decimal_comma: boolean
  places?: Record<string, string>
}
export interface Preview {
  encoding: string
  delimiter: string
  has_header: boolean
  header: string[]
  rows: string[][]
  row_count: number
  ai: boolean
  mapping: Mapping
}
export interface Problem {
  row: number
  reason: string
}
export interface DryRun {
  rows: number
  will_add: number
  skipped: Problem[]
  warnings: Problem[]
  duplicates: { row: number; name: string; matches: { id: string; title: string; path: string; why: string }[] }[]
  unmatched_places: { value: string; rows: number }[]
}
export interface Started {
  import_id: string
  job_id: string
  rows: number
}
export interface JobState {
  state: 'queued' | 'running' | 'done' | 'failed' | 'cancelled'
  last_error?: string | null
  result?: { added: number; failed: number; errors?: string[] } | null
}

/** What a column can be mapped to. */
export const targets: [string, string][] = [
  ['ignore', 'Ignore'],
  ['name', 'Name'],
  ['quantity', 'Quantity'],
  ['category', 'Category'],
  ['brand', 'Brand'],
  ['model', 'Model'],
  ['serial', 'Serial number'],
  ['color', 'Colour'],
  ['place', 'Place'],
  ['status', 'Status'],
  ['condition', 'Condition'],
  ['purchase_date', 'Bought on'],
  ['price', 'Price'],
  ['currency', 'Currency'],
  ['seller', 'Seller'],
  ['warranty_end', 'Warranty ends'],
  ['notes', 'Notes'],
]
export const dateFormats = ['YYYY-MM-DD', 'DD.MM.YYYY', 'DD/MM/YYYY', 'MM/DD/YYYY', 'DD-MM-YYYY', 'YYYY/MM/DD']

const form = (file: File, mapping?: Mapping) => {
  const f = new FormData()
  f.append('file', file)
  if (mapping) f.append('mapping', JSON.stringify(mapping))
  return f
}

export const previewImport = (file: File) => api.upload<Preview>('/api/v1/imports/preview', form(file))
export const dryRunImport = (file: File, m: Mapping) => api.upload<DryRun>('/api/v1/imports/dry-run', form(file, m))
export const startImport = (file: File, m: Mapping) => api.upload<Started>('/api/v1/imports', form(file, m))
export const undoImport = (id: string) => api.post<unknown>(`/api/v1/imports/${encodeURIComponent(id)}/undo`)
export const getJob = (id: string) => api.get<JobState>(`/api/v1/jobs/${encodeURIComponent(id)}`)

export const finished = (s: JobState['state']) => s === 'done' || s === 'failed' || s === 'cancelled'

/** Polls until the job is over; calls `onState` on every answer. Returns the last state. */
export async function waitForJob(id: string, onState: (s: JobState) => void, opts: { intervalMs?: number; get?: typeof getJob; signal?: AbortSignal } = {}): Promise<JobState> {
  const get = opts.get ?? getJob
  for (;;) {
    const s = await get(id)
    onState(s)
    if (finished(s.state) || opts.signal?.aborted) return s
    await new Promise((r) => setTimeout(r, opts.intervalMs ?? 1000))
  }
}
