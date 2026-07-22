import { describe, it, expect } from 'vitest'
import { orderTunnels, reconcileOrder } from './tunnelOrder'
import type { Tunnel } from '@/api/types'

// Only id and name matter to the ordering logic; the cast keeps the
// fixtures readable rather than restating all 18 Tunnel fields.
function t(id: string, name: string): Tunnel {
  return { id, name } as Tunnel
}

describe('orderTunnels', () => {
  it('follows the saved order rather than name order', () => {
    const tunnels = [t('1', 'alpha'), t('2', 'beta'), t('3', 'gamma')]
    const result = orderTunnels(tunnels, ['3', '1', '2'])
    expect(result.map((x) => x.id)).toEqual(['3', '1', '2'])
  })

  it('puts a tunnel missing from the saved order at the top', () => {
    const tunnels = [t('1', 'alpha'), t('2', 'beta'), t('9', 'zulu')]
    const result = orderTunnels(tunnels, ['1', '2'])
    expect(result.map((x) => x.id)).toEqual(['9', '1', '2'])
  })

  it('sorts several unknown tunnels by name, not by input order', () => {
    const tunnels = [t('9', 'zulu'), t('8', 'alpha'), t('1', 'known')]
    const result = orderTunnels(tunnels, ['1'])
    expect(result.map((x) => x.id)).toEqual(['8', '9', '1'])
  })

  it('falls back to alphabetical when nothing is saved', () => {
    const tunnels = [t('3', 'gamma'), t('1', 'alpha'), t('2', 'beta')]
    const result = orderTunnels(tunnels, [])
    expect(result.map((x) => x.name)).toEqual(['alpha', 'beta', 'gamma'])
  })

  it('ignores saved ids whose tunnel no longer exists', () => {
    const tunnels = [t('1', 'alpha'), t('3', 'gamma')]
    const result = orderTunnels(tunnels, ['1', '2', '3'])
    expect(result.map((x) => x.id)).toEqual(['1', '3'])
  })

  it('produces the same output regardless of input array order', () => {
    const saved = ['2', '1', '3']
    const a = orderTunnels([t('1', 'a'), t('2', 'b'), t('3', 'c')], saved)
    const b = orderTunnels([t('3', 'c'), t('1', 'a'), t('2', 'b')], saved)
    expect(a.map((x) => x.id)).toEqual(b.map((x) => x.id))
  })
})

describe('reconcileOrder', () => {
  it('returns null when the saved order is already correct', () => {
    const tunnels = [t('1', 'alpha'), t('2', 'beta')]
    expect(reconcileOrder(tunnels, ['1', '2'])).toBeNull()
  })

  it('prepends unknown ids sorted by name', () => {
    const tunnels = [t('1', 'known'), t('9', 'zulu'), t('8', 'alpha')]
    expect(reconcileOrder(tunnels, ['1'])).toEqual(['8', '9', '1'])
  })

  it('prunes ids whose tunnel was deleted', () => {
    const tunnels = [t('1', 'alpha'), t('3', 'gamma')]
    expect(reconcileOrder(tunnels, ['1', '2', '3'])).toEqual(['1', '3'])
  })

  it('seeds alphabetically from an empty saved order', () => {
    const tunnels = [t('3', 'gamma'), t('1', 'alpha'), t('2', 'beta')]
    expect(reconcileOrder(tunnels, [])).toEqual(['1', '2', '3'])
  })

  it('is idempotent: applying its result yields null', () => {
    const tunnels = [t('1', 'alpha'), t('9', 'zulu')]
    const first = reconcileOrder(tunnels, ['1'])
    expect(first).not.toBeNull()
    expect(reconcileOrder(tunnels, first!)).toBeNull()
  })

  it('returns null for an empty list and empty saved order', () => {
    expect(reconcileOrder([], [])).toBeNull()
  })
})
