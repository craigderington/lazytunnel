import { useState } from 'react'
import { useTunnels, useStartTunnel, useStopTunnel, useDeleteTunnel } from '@/lib/queries'
import { useTunnelStore } from '@/store/tunnelStore'
import { PageHeader } from './PageHeader'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import type { Tunnel, TunnelStatus } from '@/api/types'
import { Play, Square, Trash2, Pencil, Loader2, ArrowRight } from 'lucide-react'
import { cn } from '@/lib/utils'
import { getTunnelBrowseUrl } from '@/lib/tunnelUrl'
import { EditTunnelDialog } from './EditTunnelDialog'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from './ui/alert-dialog'

export function TunnelList() {
  const { isLoading, error } = useTunnels()
  const tunnels = useTunnelStore((s) => s.tunnels)
  const isDemoMode = useTunnelStore((s) => s.isDemoMode)
  const startTunnel = useStartTunnel()
  const stopTunnel = useStopTunnel()
  const deleteTunnel = useDeleteTunnel()
  const [busy, setBusy] = useState<string | null>(null)
  const [editing, setEditing] = useState<Tunnel | null>(null)
  const [confirmingDelete, setConfirmingDelete] = useState<Tunnel | null>(null)

  if (isLoading && tunnels.length === 0) {
    return (
      <div className="flex h-48 items-center justify-center">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (error && !isDemoMode) {
    return (
      <p className="text-sm text-destructive">
        {error instanceof Error ? error.message : 'Could not load tunnels'}
      </p>
    )
  }

  if (tunnels.length === 0) {
    return (
      <>
        <PageHeader title="Tunnels" description="No tunnels yet." />
        <p className="text-sm text-muted-foreground">
          Create one with + New, or try demo mode.
        </p>
      </>
    )
  }

  const active = tunnels.filter((t) => t.status === 'active').length

  return (
    <>
      <PageHeader
        title="Tunnels"
        description={`${active} of ${tunnels.length} active`}
      />

      <ul className="divide-y divide-border border-t border-border">
        {[...tunnels]
          .sort((a, b) => a.name.localeCompare(b.name))
          .map((tunnel) => (
            <TunnelRow
              key={tunnel.id}
              tunnel={tunnel}
              busy={busy === tunnel.id}
              onStart={() => {
                setBusy(tunnel.id)
                startTunnel.mutate(tunnel.id, { onSettled: () => setBusy(null) })
              }}
              onStop={() => {
                setBusy(tunnel.id)
                stopTunnel.mutate(tunnel.id, { onSettled: () => setBusy(null) })
              }}
              onEdit={() => setEditing(tunnel)}
              onDelete={() => setConfirmingDelete(tunnel)}
            />
          ))}
      </ul>

      {editing && (
        <EditTunnelDialog
          key={editing.id}
          tunnel={editing}
          open
          onOpenChange={(o) => {
            if (!o) setEditing(null)
          }}
        />
      )}

      <AlertDialog
        open={confirmingDelete !== null}
        onOpenChange={(o) => {
          if (!o) setConfirmingDelete(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              Delete {confirmingDelete?.name}?
            </AlertDialogTitle>
            <AlertDialogDescription>
              This permanently removes the tunnel and its configuration. If it
              is running it will be stopped first. This cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                const target = confirmingDelete
                if (!target) return
                setConfirmingDelete(null)
                setBusy(target.id)
                deleteTunnel.mutate(target.id, {
                  onSettled: () => setBusy(null),
                })
              }}
            >
              Delete tunnel
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

function TunnelRow({
  tunnel,
  busy,
  onStart,
  onStop,
  onEdit,
  onDelete,
}: {
  tunnel: Tunnel
  busy: boolean
  onStart: () => void
  onStop: () => void
  onEdit: () => void
  onDelete: () => void
}) {
  const status = statusLabel(tunnel.status)
  const browseUrl = getTunnelBrowseUrl(tunnel)
  const endpoint = `${tunnel.remoteHost}:${tunnel.remotePort}`
  const canEdit = tunnel.status !== 'active' && tunnel.status !== 'connecting'

  return (
    <li
      className={cn(
        'flex flex-col gap-4 py-5 sm:flex-row sm:items-center sm:justify-between',
        tunnel.status === 'active' && 'bg-primary/[0.03]'
      )}
    >
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-3">
          <span className="font-medium">{tunnel.name}</span>
          <Badge
            variant={status.variant}
            className="font-mono text-[10px] font-normal uppercase tracking-wider"
          >
            {status.label}
          </Badge>
        </div>
        <p className="mt-2 flex flex-wrap items-center gap-2 font-mono text-xs text-muted-foreground">
          <span>:{tunnel.localPort}</span>
          <ArrowRight className="h-3 w-3" />
          {browseUrl ? (
            <a
              href={browseUrl}
              target="_blank"
              rel="noopener noreferrer"
              title={`Open via tunnel (${browseUrl})`}
              className="rounded-sm text-muted-foreground underline-offset-2 transition-colors hover:bg-muted/40 hover:text-foreground hover:underline decoration-muted-foreground/50"
            >
              {endpoint}
            </a>
          ) : (
            <span>{endpoint}</span>
          )}
          {tunnel.agentId && (
            <span className="text-muted-foreground/70">agent {tunnel.agentId}</span>
          )}
          {tunnel.hops[0] && (
            <span className="text-muted-foreground/70">
              via {tunnel.hops[0].user}@{tunnel.hops[0].host}
            </span>
          )}
        </p>
        {tunnel.errorMessage && (
          <p className="mt-2 text-xs text-destructive">{tunnel.errorMessage}</p>
        )}
      </div>

      <div className="flex shrink-0 gap-2">
        {tunnel.status === 'active' ? (
          <Button variant="outline" size="sm" onClick={onStop} disabled={busy}>
            {busy ? <Loader2 className="h-3 w-3 animate-spin" /> : <Square className="h-3 w-3" />}
            <span className="ml-1.5">Stop</span>
          </Button>
        ) : (
          <Button size="sm" onClick={onStart} disabled={busy || tunnel.status === 'connecting'}>
            {busy || tunnel.status === 'connecting' ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : (
              <Play className="h-3 w-3" />
            )}
            <span className="ml-1.5">Start</span>
          </Button>
        )}
        <Button
          variant="ghost"
          size="sm"
          onClick={onEdit}
          disabled={busy || !canEdit}
          title={canEdit ? 'Edit tunnel' : 'Stop the tunnel to edit'}
        >
          <Pencil className="h-3 w-3 text-muted-foreground" />
        </Button>
        <Button variant="ghost" size="sm" onClick={onDelete} disabled={busy}>
          <Trash2 className="h-3 w-3 text-muted-foreground" />
        </Button>
      </div>
    </li>
  )
}

function statusLabel(status: TunnelStatus) {
  switch (status) {
    case 'active':
      return { label: 'live', variant: 'success' as const }
    case 'connecting':
      return { label: 'connecting', variant: 'warning' as const }
    case 'failed':
      return { label: 'failed', variant: 'destructive' as const }
    default:
      return { label: status, variant: 'secondary' as const }
  }
}