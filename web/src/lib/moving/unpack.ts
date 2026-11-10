// Pure parts of unpacking one box: what the person decided for each thing.
export interface Choice {
  /** A place id, or `null` for "not found". An undecided thing has no entry. */
  place: string | null
}
export type Choices = Record<string, Choice>

export interface Move {
  item_id?: string
  line_id?: string
  place_id: string | null
}

export interface Thing {
  id: string
  kind: 'item' | 'line'
}

/** POST /boxes/{id}/unpack moves for everything decided so far, in list order. */
export function toMoves(things: Thing[], choices: Choices): Move[] {
  const out: Move[] = []
  for (const t of things) {
    const c = choices[t.id]
    if (c) out.push({ [t.kind === 'item' ? 'item_id' : 'line_id']: t.id, place_id: c.place })
  }
  return out
}

/** Gives every undecided thing `place` ("put everything in the room"). */
export function fillRest(things: Thing[], choices: Choices, place: string): Choices {
  const next = { ...choices }
  for (const t of things) if (!next[t.id]) next[t.id] = { place }
  return next
}

export const undecided = (things: Thing[], choices: Choices) => things.filter((t) => !choices[t.id]).length
