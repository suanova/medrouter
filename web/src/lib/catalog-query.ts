// Catalog requests cross the platform edge, which can reset an individual
// request while the rest of the page loads (observed as ERR_CONNECTION_RESET on
// one XHR, with the origin serving it fine). A couple of backed-off retries turn
// a transient reset into a slower load instead of "catalog unavailable".
export const CATALOG_RETRY_COUNT = 3

// catalogRetryDelay backs off 1s, 2s, 4s, ... and caps at 8s.
export function catalogRetryDelay(attemptIndex: number): number {
  return Math.min(1000 * 2 ** attemptIndex, 8000)
}
