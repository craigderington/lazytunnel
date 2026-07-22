import type { Tunnel } from '@/api/types'

function byName(a: Tunnel, b: Tunnel): number {
  return a.name.localeCompare(b.name)
}

/**
 * Applies a saved order to the polled tunnel list.
 *
 * Saved ids render in their saved sequence; anything the saved order has
 * never seen goes to the top, sorted by name. The name sort matters: the
 * API returns tunnels in a nondeterministic order, so any unsorted fallback
 * would let rows shuffle on every 5s poll.
 */
export function orderTunnels(tunnels: Tunnel[], saved: string[]): Tunnel[] {
  const byId = new Map(tunnels.map((t) => [t.id, t]))
  const savedSet = new Set(saved)

  const known = saved
    .map((id) => byId.get(id))
    .filter((t): t is Tunnel => t !== undefined)

  const unknown = tunnels.filter((t) => !savedSet.has(t.id)).sort(byName)

  return [...unknown, ...known]
}

/**
 * Computes the order to persist, or null when the saved order already
 * matches. Returning null is load-bearing: the caller writes to a store on
 * a non-null result, so always returning an array would loop.
 *
 * An empty saved order needs no special case — every tunnel is then
 * "unknown", so the name sort seeds the alphabetical order the list showed
 * before this feature existed.
 */
export function reconcileOrder(
  tunnels: Tunnel[],
  saved: string[]
): string[] | null {
  // A transiently-empty tunnel list must never prune a saved order down to
  // nothing. The tunnel store is empty until the first poll resolves, so on
  // every page load `tunnels` is briefly `[]`; without this guard the effect
  // would persist `[]` and wipe the user's custom order. If every tunnel was
  // genuinely deleted the stale ids are harmless — orderTunnels ignores ids
  // with no matching tunnel, and they self-heal on the next create.
  if (tunnels.length === 0 && saved.length > 0) {
    return null
  }

  const byId = new Map(tunnels.map((t) => [t.id, t]))
  const savedSet = new Set(saved)

  const kept = saved.filter((id) => byId.has(id))
  const unknownIds = tunnels
    .filter((t) => !savedSet.has(t.id))
    .sort(byName)
    .map((t) => t.id)

  const next = [...unknownIds, ...kept]

  if (next.length === saved.length && next.every((id, i) => id === saved[i])) {
    return null
  }

  return next
}
