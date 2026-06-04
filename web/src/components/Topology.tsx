import { useEffect, useMemo, useState } from 'react'
import { useTunnelStore } from '@/store/tunnelStore'
import { PageHeader } from './PageHeader'
import { cn } from '@/lib/utils'
import type { Tunnel } from '@/api/types'

export function Topology() {
  const tunnels = useTunnelStore((s) => s.tunnels)
  const [selectedId, setSelectedId] = useState<string | null>(null)

  useEffect(() => {
    if (!tunnels.length) {
      setSelectedId(null)
      return
    }
    if (!selectedId || !tunnels.some((t) => t.id === selectedId)) {
      setSelectedId(tunnels[0].id)
    }
  }, [tunnels, selectedId])

  const selected = useMemo(
    () => tunnels.find((t) => t.id === selectedId) ?? tunnels[0],
    [tunnels, selectedId]
  )

  if (!tunnels.length) {
    return (
      <>
        <PageHeader title="Topology" description="Hop chain visualization" />
        <p className="text-sm text-muted-foreground">No tunnels to map.</p>
      </>
    )
  }

  return (
    <>
      <PageHeader
        title="Topology"
        description="How traffic flows through each tunnel"
      />

      <div className="mb-6 flex flex-wrap gap-2">
        {tunnels.map((t) => (
          <button
            key={t.id}
            type="button"
            onClick={() => setSelectedId(t.id)}
            className={cn(
              'rounded-md border px-3 py-1.5 font-mono text-xs transition-colors',
              selected?.id === t.id
                ? 'border-foreground bg-foreground text-background'
                : 'border-border text-muted-foreground hover:text-foreground'
            )}
          >
            {t.name}
          </button>
        ))}
      </div>

      {selected && <TunnelGraph tunnel={selected} />}
    </>
  )
}

function TunnelGraph({ tunnel }: { tunnel: Tunnel }) {
  const nodes = useMemo(() => buildNodes(tunnel), [tunnel])
  const active = tunnel.status === 'active'
  const spacing = 150
  const width = Math.max(720, nodes.length * spacing)
  const height = 140
  const y = 70

  return (
    <div className="overflow-x-auto rounded-lg border border-border">
      <svg
        viewBox={`0 0 ${width} ${height}`}
        className="w-full min-w-[720px] text-foreground"
        role="img"
        aria-label={`Topology for ${tunnel.name}`}
      >
        {nodes.slice(0, -1).map((_, i) => {
          const x1 = 90 + i * spacing
          const x2 = x1 + spacing
          return (
            <g key={`edge-${i}`}>
              <line
                x1={x1}
                y1={y}
                x2={x2}
                y2={y}
                stroke="currentColor"
                strokeOpacity={active ? 0.35 : 0.15}
                strokeWidth={1.5}
              />
              <polygon
                points={`${x2 - 6},${y - 4} ${x2},${y} ${x2 - 6},${y + 4}`}
                className={cn(
                  active ? 'fill-[hsl(var(--live))]' : 'fill-muted-foreground/30'
                )}
              />
            </g>
          )
        })}

        {nodes.map((node, i) => {
          const x = 90 + i * spacing
          const lit =
            active &&
            (node.kind === 'local' ||
              node.kind === 'target' ||
              node.kind === 'agent')
          return (
            <g key={node.id} transform={`translate(${x}, ${y})`}>
              <circle
                r={8}
                className={cn(
                  'stroke-background stroke-[2px]',
                  lit ? 'fill-[hsl(var(--live))]' : 'fill-muted-foreground/35'
                )}
              />
              <text
                y={-20}
                textAnchor="middle"
                className="fill-foreground font-mono text-[10px] font-medium"
              >
                {node.label}
              </text>
              <text
                y={24}
                textAnchor="middle"
                className="fill-muted-foreground font-mono text-[9px]"
              >
                {node.sub}
              </text>
            </g>
          )
        })}
      </svg>

      <div className="border-t border-border px-4 py-3 font-mono text-xs text-muted-foreground">
        <span className="text-foreground">{tunnel.name}</span>
        {' · '}
        {tunnel.type} · {tunnel.status}
        {tunnel.agentId && ` · agent ${tunnel.agentId}`}
        {tunnel.hops.length > 0 &&
          ` · ${tunnel.hops.length} hop${tunnel.hops.length === 1 ? '' : 's'}`}
      </div>
    </div>
  )
}

function buildNodes(tunnel: Tunnel) {
  const nodes: { id: string; label: string; sub: string; kind: string }[] = [
    {
      id: 'local',
      label: 'you',
      sub: `127.0.0.1:${tunnel.localPort}`,
      kind: 'local',
    },
  ]

  if (tunnel.agentId) {
    nodes.push({
      id: 'agent',
      label: 'agent',
      sub: tunnel.agentId,
      kind: 'agent',
    })
  }

  if (tunnel.hops.length === 0) {
    nodes.push({
      id: 'hop-0',
      label: 'ssh',
      sub: 'direct',
      kind: 'hop',
    })
  } else {
    tunnel.hops.forEach((hop, i) => {
      nodes.push({
        id: `hop-${i}`,
        label: hop.user || `hop ${i + 1}`,
        sub: `${hop.host}:${hop.port}`,
        kind: 'hop',
      })
    })
  }

  nodes.push({
    id: 'target',
    label: tunnel.remoteHost || 'target',
    sub: `:${tunnel.remotePort}`,
    kind: 'target',
  })

  return nodes
}