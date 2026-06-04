import { useEffect } from 'react'
import { useConnectionStore } from '@/store/connectionStore'
import { useAuthStore } from '@/store/authStore'
import { useTunnelStore } from '@/store/tunnelStore'
import { api } from '@/api/client'
import { cn } from '@/lib/utils'

function Indicator({
  label,
  ok,
  pending,
}: {
  label: string
  ok: boolean | null
  pending?: boolean
}) {
  return (
    <span className="inline-flex items-center gap-2 text-muted-foreground">
      <span
        className={cn(
          'h-1.5 w-1.5 rounded-full transition-colors duration-300',
          pending && 'animate-pulse bg-muted-foreground/40',
          !pending && ok === true && 'bg-[hsl(var(--live))] shadow-[0_0_6px_hsl(var(--live)/0.8)]',
          !pending && ok === false && 'bg-destructive',
          !pending && ok === null && 'bg-muted-foreground/30'
        )}
      />
      <span className="text-[11px] uppercase tracking-wider">{label}</span>
    </span>
  )
}

export function SystemStatusBar() {
  const apiHealthy = useConnectionStore((s) => s.apiHealthy)
  const wsConnected = useConnectionStore((s) => s.wsConnected)
  const setApiHealthy = useConnectionStore((s) => s.setApiHealthy)
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated)
  const authRequired = useAuthStore((s) => s.authRequired)
  const tunnels = useTunnelStore((s) => s.tunnels)
  const isDemoMode = useTunnelStore((s) => s.isDemoMode)

  const active = tunnels.filter((t) => t.status === 'active').length

  useEffect(() => {
    if (isDemoMode) {
      setApiHealthy(true)
      return
    }

    let cancelled = false
    const check = async () => {
      try {
        await api.getHealth()
        if (!cancelled) setApiHealthy(true)
      } catch {
        if (!cancelled) setApiHealthy(false)
      }
    }
    check()
    const id = setInterval(check, 15000)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [setApiHealthy, isDemoMode])

  const liveOk = isDemoMode ? null : isAuthenticated ? wsConnected : null
  const livePending = isAuthenticated && !isDemoMode && !wsConnected

  return (
    <div className="flex flex-wrap items-center gap-6 border-t border-border/60 py-2.5 font-mono text-[11px]">
      <Indicator label="API" ok={isDemoMode ? true : apiHealthy} />
      <Indicator label="Live" ok={liveOk} pending={livePending} />
      {authRequired && (
        <span className="text-muted-foreground">
          {isAuthenticated ? 'Signed in' : 'Signed out'}
        </span>
      )}
      <span className="ml-auto text-foreground/80">
        {active} / {tunnels.length} active
      </span>
      {isDemoMode && (
        <span className="text-muted-foreground">demo</span>
      )}
    </div>
  )
}