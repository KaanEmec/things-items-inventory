import type { QueueEntry, Role } from './queue.svelte'

export const roleLabel: Record<Role, string> = { item: 'Item', label: 'Label', receipt: 'Receipt', contents: 'Contents' }

export const stateLabel: Record<QueueEntry['state'], string> = {
  waiting: 'Waiting',
  uploading: 'Uploading',
  uploaded: 'Uploaded',
  failed: 'Failed',
}

export const stateClass: Record<QueueEntry['state'], string> = {
  waiting: 'bg-stored-bg text-stored-fg',
  uploading: 'bg-sealed-bg text-sealed-fg',
  uploaded: 'bg-use-bg text-use-fg',
  failed: 'bg-repair-bg text-repair-fg',
}

/** A group's one-word summary: failed beats uploading beats waiting beats uploaded. */
export function groupState(es: QueueEntry[]): QueueEntry['state'] {
  for (const s of ['failed', 'uploading', 'waiting'] as const) if (es.some((e) => e.state === s)) return s
  return 'uploaded'
}
