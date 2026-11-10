// The update flow as a small state machine. A new service worker installs and waits ("waiting");
// the person taps Reload ("reload"), which tells it to take over; when it controls the page
// ("controlled") the page reloads. Nothing swaps without that tap.
export type UpdateState = 'idle' | 'available' | 'applying'
export type UpdateEvent = 'waiting' | 'reload' | 'controlled' | 'failed'

export function nextUpdate(state: UpdateState, event: UpdateEvent): UpdateState {
  switch (state) {
    case 'idle':
      return event === 'waiting' ? 'available' : state
    case 'available':
      return event === 'reload' ? 'applying' : state
    case 'applying':
      if (event === 'controlled') return 'idle'
      if (event === 'failed') return 'available'
      return state
  }
}
