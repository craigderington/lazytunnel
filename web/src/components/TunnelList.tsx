import { useTunnels, useStartTunnel, useStopTunnel, useDeleteTunnel } from '@/lib/queries'
import { useTunnelStore } from '@/store/tunnelStore'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './ui/card'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import type { TunnelSpec, TunnelStatus } from '@/types/tunnel'
import {
  Play,
  Square,
  Trash2,
  Activity,
  Clock,
  AlertCircle,
  CheckCircle2,
  Loader2,
  ArrowRight,
  ExternalLink,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { useState } from 'react'

export function TunnelList() {
  const { isLoading, error } = useTunnels()
  const tunnels = useTunnelStore((state) => state.tunnels)
  const isDemoMode = useTunnelStore((state) => state.isDemoMode)
  const startTunnel = useStartTunnel()
  const stopTunnel = useStopTunnel()
  const deleteTunnel = useDeleteTunnel()

  const [startingTunnelId, setStartingTunnelId] = useState<string | null>(null)
  const [stoppingTunnelId, setStoppingTunnelId] = useState<string | null>(null)
  const [deletingTunnelId, setDeletingTunnelId] = useState<string | null>(null)

  console.log('🔍 TunnelList Debug:', {
    tunnels: tunnels.length,
    isDemoMode,
    isLoading,
    error: error?.message
  })

  if (isLoading && tunnels.length === 0) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader2 className="h-8 w-8 animate-spin text-primary" />
      </div>
    )
  }

  if (error && !isDemoMode) {
    return (
      <Card className="border-destructive">
        <CardHeader>
          <CardTitle className="text-destructive">Error Loading Tunnels</CardTitle>
          <CardDescription>
            {error instanceof Error ? error.message : 'Failed to load tunnels'}
          </CardDescription>
          <CardDescription className="mt-2">
            💡 Tip: Make sure the backend server is running on port 8080, or click "Demo Mode" to see sample tunnels.
          </CardDescription>
        </CardHeader>
      </Card>
    )
  }

  if (!tunnels || tunnels.length === 0) {
    console.log('📭 No tunnels to display')
    return (
      <Card>
        <CardHeader>
          <CardTitle>No Tunnels</CardTitle>
          <CardDescription>
            Create your first tunnel to get started, or click "Demo Mode" to see sample tunnels
          </CardDescription>
        </CardHeader>
      </Card>
    )
  }

  console.log('✅ Displaying', tunnels.length, 'tunnels')

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-3xl font-bold tracking-tight">Active Tunnels</h2>
          <p className="text-muted-foreground">
            Manage your SSH port forwards
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Badge variant="secondary" className="text-sm">
            {tunnels.filter(t => t.status === 'active').length} Active
          </Badge>
          <Badge variant="outline" className="text-sm">
            {tunnels.length} Total
          </Badge>
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {tunnels.map((tunnel) => (
          <TunnelCard
            key={tunnel.id}
            tunnel={tunnel}
            onStart={() => {
              console.log('🎬 START clicked for:', tunnel.name, tunnel.id)
              setStartingTunnelId(tunnel.id)
              startTunnel.mutate(tunnel.id, {
                onSettled: () => {
                  console.log('🏁 START settled for:', tunnel.id)
                  setStartingTunnelId(null)
                }
              })
            }}
            onStop={() => {
              console.log('🎬 STOP clicked for:', tunnel.name, tunnel.id)
              setStoppingTunnelId(tunnel.id)
              stopTunnel.mutate(tunnel.id, {
                onSettled: () => {
                  console.log('🏁 STOP settled for:', tunnel.id)
                  setStoppingTunnelId(null)
                }
              })
            }}
            onDelete={() => {
              console.log('🎬 DELETE clicked for:', tunnel.name, tunnel.id)
              setDeletingTunnelId(tunnel.id)
              deleteTunnel.mutate(tunnel.id, {
                onSettled: () => {
                  console.log('🏁 DELETE settled for:', tunnel.id)
                  setDeletingTunnelId(null)
                }
              })
            }}
            isStarting={startingTunnelId === tunnel.id}
            isStopping={stoppingTunnelId === tunnel.id}
            isDeleting={deletingTunnelId === tunnel.id}
          />
        ))}
      </div>
    </div>
  )
}

interface TunnelCardProps {
  tunnel: TunnelSpec
  onStart: () => void
  onStop: () => void
  onDelete: () => void
  isStarting: boolean
  isStopping: boolean
  isDeleting: boolean
}

function TunnelCard({
  tunnel,
  onStart,
  onStop,
  onDelete,
  isStarting,
  isStopping,
  isDeleting,
}: TunnelCardProps) {
  const statusConfig = getStatusConfig(tunnel.status)

  return (
    <Card className={cn(
      "transition-all hover:shadow-lg",
      tunnel.status === 'active' && "border-green-500/50"
    )}>
      <CardHeader className="pb-3">
        <div className="flex items-start justify-between">
          <div className="flex-1">
            <CardTitle className="flex items-center gap-2">
              {tunnel.name}
              <Badge
                variant={statusConfig.variant}
                className="ml-auto"
              >
                <statusConfig.icon className="mr-1 h-3 w-3" />
                {statusConfig.label}
              </Badge>
            </CardTitle>
            <CardDescription className="mt-1">
              {tunnel.type.toUpperCase()} • Port {tunnel.localPort}
            </CardDescription>
          </div>
        </div>
      </CardHeader>

      <CardContent className="space-y-3">
        {/* Connection Path */}
        <div className="space-y-2">
          <div className="text-xs font-medium text-muted-foreground">
            Connection Path
          </div>
          <div className="flex items-center gap-2 text-sm">
            <a
              href={`http://localhost:${tunnel.localPort}`}
              target="_blank"
              rel="noopener noreferrer"
              className={cn(
                "rounded bg-muted px-2 py-1 inline-flex items-center gap-1 transition-colors",
                tunnel.status === 'active'
                  ? "hover:bg-primary hover:text-primary-foreground cursor-pointer"
                  : "opacity-50 cursor-not-allowed pointer-events-none"
              )}
              onClick={(e) => {
                if (tunnel.status !== 'active') {
                  e.preventDefault()
                }
              }}
            >
              <code>localhost:{tunnel.localPort}</code>
              {tunnel.status === 'active' && (
                <ExternalLink className="h-3 w-3" />
              )}
            </a>
            <ArrowRight className="h-3 w-3 text-muted-foreground" />
            <code className="rounded bg-muted px-2 py-1 truncate">
              {tunnel.remoteHost}:{tunnel.remotePort}
            </code>
          </div>
        </div>

        {/* Hops */}
        {tunnel.hops.length > 0 && (
          <div className="space-y-1">
            <div className="text-xs font-medium text-muted-foreground">
              Via {tunnel.hops.length} hop{tunnel.hops.length > 1 ? 's' : ''}
            </div>
            <div className="flex flex-wrap gap-1">
              {tunnel.hops.map((hop, i) => (
                <Badge key={i} variant="outline" className="text-xs">
                  {hop.user}@{hop.host}
                </Badge>
              ))}
            </div>
          </div>
        )}

        {/* Error Message */}
        {tunnel.errorMessage && (
          <div className="rounded-md bg-destructive/10 p-2 text-xs text-destructive">
            {tunnel.errorMessage}
          </div>
        )}

        {/* Actions */}
        <div className="flex gap-2 pt-2">
          {tunnel.status === 'active' ? (
            <Button
              variant="outline"
              size="sm"
              className="flex-1"
              onClick={onStop}
              disabled={isStopping}
            >
              {isStopping ? (
                <Loader2 className="mr-2 h-3 w-3 animate-spin" />
              ) : (
                <Square className="mr-2 h-3 w-3" />
              )}
              {isStopping ? "Stopping..." : "Stop"}
            </Button>
          ) : tunnel.status === 'connecting' || isStarting ? (
            <Button
              size="sm"
              className="flex-1"
              disabled={true}
            >
              <Loader2 className="mr-2 h-3 w-3 animate-spin" />
              Connecting...
            </Button>
          ) : (
            <Button
              size="sm"
              className="flex-1"
              onClick={onStart}
              disabled={isStarting}
            >
              <Play className="mr-2 h-3 w-3" />
              Start
            </Button>
          )}
          <Button
            variant="ghost"
            size="sm"
            onClick={onDelete}
            disabled={isDeleting}
          >
            {isDeleting ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <Trash2 className="h-4 w-4 text-destructive" />
            )}
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}

function getStatusConfig(status: TunnelStatus) {
  switch (status) {
    case 'active':
      return {
        label: 'Active',
        variant: 'success' as const,
        icon: CheckCircle2,
      }
    case 'connecting':
      return {
        label: 'Connecting',
        variant: 'warning' as const,
        icon: Clock,
      }
    case 'stopped':
      return {
        label: 'Stopped',
        variant: 'secondary' as const,
        icon: Square,
      }
    case 'disconnected':
      return {
        label: 'Disconnected',
        variant: 'secondary' as const,
        icon: AlertCircle,
      }
    case 'failed':
      return {
        label: 'Failed',
        variant: 'destructive' as const,
        icon: AlertCircle,
      }
    default:
      return {
        label: 'Unknown',
        variant: 'outline' as const,
        icon: Activity,
      }
  }
}
