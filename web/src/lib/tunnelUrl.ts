import type { Tunnel } from '@/api/types'

/** URL to open in the browser when the tunnel is active (local forward bind). */
export function getTunnelBrowseUrl(tunnel: Tunnel): string | null {
  if (tunnel.status !== 'active' || tunnel.localPort < 1) {
    return null
  }

  const scheme = tunnel.localPort === 443 ? 'https' : 'http'
  const host = '127.0.0.1'

  if (tunnel.localPort === 80 || tunnel.localPort === 443) {
    return `${scheme}://${host}`
  }

  return `${scheme}://${host}:${tunnel.localPort}`
}