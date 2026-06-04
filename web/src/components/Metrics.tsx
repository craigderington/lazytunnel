import { useMemo } from 'react'
import { useTunnelStore } from '@/store/tunnelStore'
import { useActiveTunnelMetrics } from '@/lib/queries'
import { PageHeader } from './PageHeader'
import type { Tunnel, TunnelMetrics } from '@/api/types'
import { cn } from '@/lib/utils'

export function Metrics() {
  const { tunnels, isDemoMode } = useTunnelStore()
  const activeTunnels = useMemo(
    () => tunnels.filter((t) => t.status === 'active'),
    [tunnels]
  )
  const activeIds = useMemo(
    () => activeTunnels.map((t) => t.id),
    [activeTunnels]
  )

  const { data: liveMetrics = [], isLoading } = useActiveTunnelMetrics(
    activeIds,
    !isDemoMode
  )

  const metricsById = useMemo(() => {
    const map = new Map<string, TunnelMetrics>()
    if (isDemoMode) {
      activeTunnels.forEach((t, i) => map.set(t.id, demoMetrics(t, i)))
      return map
    }
    liveMetrics.forEach((m) => map.set(m.tunnelId, m))
    return map
  }, [isDemoMode, activeTunnels, liveMetrics])

  const fleet = useMemo(() => {
    const rows = activeTunnels.map((t) => ({
      tunnel: t,
      metrics: metricsById.get(t.id),
    }))
    let bytesIn = 0
    let bytesOut = 0
    let connections = 0
    for (const { metrics } of rows) {
      if (!metrics) continue
      bytesIn += metrics.bytesIn
      bytesOut += metrics.bytesOut
      connections += metrics.connectionsActive
    }
    return {
      rows,
      bytesIn,
      bytesOut,
      connections,
      total: tunnels.length,
      active: activeTunnels.length,
      failed: tunnels.filter((t) => t.status === 'failed').length,
    }
  }, [activeTunnels, metricsById, tunnels])

  return (
    <>
      <PageHeader
        title="Metrics"
        description="Traffic and uptime from active tunnels"
      />

      <div className="grid grid-cols-2 gap-px overflow-hidden rounded-lg border border-border bg-border sm:grid-cols-4">
        <Stat label="Active" value={String(fleet.active)} accent />
        <Stat label="Bytes in" value={formatBytes(fleet.bytesIn)} />
        <Stat label="Bytes out" value={formatBytes(fleet.bytesOut)} />
        <Stat
          label="Connections"
          value={fleet.active ? String(fleet.connections) : '—'}
        />
      </div>

      <section className="mt-10">
        <h2 className="mb-4 text-xs uppercase tracking-wider text-muted-foreground">
          Per tunnel
        </h2>

        {fleet.active === 0 ? (
          <p className="text-sm text-muted-foreground">
            No active tunnels — start one to see bytes and uptime here.
          </p>
        ) : (
          <div className="overflow-x-auto rounded-lg border border-border">
            <table className="w-full min-w-[520px] text-left font-mono text-xs">
              <thead>
                <tr className="border-b border-border text-muted-foreground">
                  <th className="px-4 py-3 font-sans font-medium">Tunnel</th>
                  <th className="px-4 py-3">In</th>
                  <th className="px-4 py-3">Out</th>
                  <th className="px-4 py-3">Uptime</th>
                  <th className="px-4 py-3">Conns</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {fleet.rows.map(({ tunnel, metrics }) => (
                  <tr key={tunnel.id} className="bg-card">
                    <td className="px-4 py-3 font-sans text-sm text-foreground">
                      {tunnel.name}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {metrics ? formatBytes(metrics.bytesIn) : isLoading ? '…' : '—'}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {metrics ? formatBytes(metrics.bytesOut) : isLoading ? '…' : '—'}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {metrics ? formatUptime(metrics.uptime) : isLoading ? '…' : '—'}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {metrics != null ? metrics.connectionsActive : isLoading ? '…' : '—'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {fleet.failed > 0 && (
        <p className="mt-6 text-xs text-muted-foreground">
          {fleet.failed} failed tunnel{fleet.failed === 1 ? '' : 's'} — not included
          above.
        </p>
      )}
    </>
  )
}

function Stat({
  label,
  value,
  accent,
}: {
  label: string
  value: string
  accent?: boolean
}) {
  return (
    <div className="bg-card px-5 py-6">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p
        className={cn(
          'mt-2 text-xl font-medium tabular-nums',
          accent && 'text-[hsl(var(--live))]'
        )}
      >
        {value}
      </p>
    </div>
  )
}

function formatBytes(n: number): string {
  if (n === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB'] as const
  const i = Math.min(
    Math.floor(Math.log(n) / Math.log(1024)),
    units.length - 1
  )
  const v = n / 1024 ** i
  return `${v < 10 && i > 0 ? v.toFixed(1) : Math.round(v)} ${units[i]}`
}

function formatUptime(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  if (seconds < 86400) {
    const h = Math.floor(seconds / 3600)
    const m = Math.floor((seconds % 3600) / 60)
    return m ? `${h}h ${m}m` : `${h}h`
  }
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  return h ? `${d}d ${h}h` : `${d}d`
}

function demoMetrics(tunnel: Tunnel, index: number): TunnelMetrics {
  const base = (index + 1) * 1_200_000
  return {
    tunnelId: tunnel.id,
    bytesIn: base * 47,
    bytesOut: base * 12,
    connectionsActive: index === 2 ? 3 : 1,
    uptime: 3600 * (index + 2) + 900,
    lastHeartbeat: new Date().toISOString(),
  }
}