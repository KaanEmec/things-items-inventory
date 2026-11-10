import type { Item } from '../inventory/types'

/** What is being dragged right now (dragover cannot read dataTransfer, so the screen keeps it here). */
export const drag = $state<{ what: { kind: 'place'; id: string } | { kind: 'items'; items: Item[] } | null }>({ what: null })

export const startDrag = (what: NonNullable<typeof drag.what>, e: DragEvent) => {
  drag.what = what
  if (e.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', what.kind === 'place' ? what.id : what.items.map((i) => i.id).join(','))
  }
}
export const endDrag = () => (drag.what = null)
