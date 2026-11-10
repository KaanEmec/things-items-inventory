// What the app knows about its link to the server, shown by ConnectionBanner.
// api.ts reports every response here; auth.svelte.ts reports a 401 on a signed-in screen.
export const conn = $state({
  /** The server answered 502/503/504 or could not be reached while the device is online (usually an update). */
  serverUpdating: false,
  /** A signed-in request came back 401. The screen stays so nothing typed is lost. */
  sessionEnded: false,
})

/** Called by the api layer with each outcome: an HTTP status, or 0 when the server could not be reached. */
export function noteResponse(status: number) {
  const down = status === 502 || status === 503 || status === 504 || (status === 0 && (typeof navigator === 'undefined' || navigator.onLine !== false))
  if (conn.serverUpdating !== down) conn.serverUpdating = down
}
