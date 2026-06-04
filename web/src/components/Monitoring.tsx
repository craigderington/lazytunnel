import { useMemo, useState } from 'react'
import { useTunnelStore } from '@/store/tunnelStore'
import { PageHeader } from './PageHeader'
import { LogPanel } from './LogPanel'
import { Button } from './ui/button'
import { cn } from '@/lib/utils'

interface Event {
  id: string
  type: 'connected' | 'failed' | 'disconnected' | 'created'
  at: Date
  tunnelName: string
  message: string
}

export function Monitoring() {
  const { tunnels } = useTunnelStore()
  const [showLogs, setShowLogs] = useState(false)

  const events = useMemo(() => {
    const list: Event[] = []
    for (const t of tunnels) {
      list.push({
        id: `${t.id}-c`,
        type: 'created',
        at: new Date(t.createdAt),
        tunnelName: t.name,
        message: `Created`,
      })
      if (t.status === 'active') {
        list.push({
          id: `${t.id}-a`,
          type: 'connected',
          at: new Date(t.updatedAt),
          tunnelName: t.name,
          message: `Connected`,
        })
      }
      if (t.status === 'failed') {
        list.push({
          id: `${t.id}-f`,
          type: 'failed',
          at: new Date(t.updatedAt),
          tunnelName: t.name,
          message: t.errorMessage || 'Failed',
        })
      }
      if (t.status === 'disconnected' || t.status === 'stopped') {
        list.push({
          id: `${t.id}-d`,
          type: 'disconnected',
          at: new Date(t.updatedAt),
          tunnelName: t.name,
          message: `Stopped`,
        })
      }
    }
    return list.sort((a, b) => b.at.getTime() - a.at.getTime()).slice(0, 40)
  }, [tunnels])

  return (
    <>
      <PageHeader
        title="Activity"
        description="Recent tunnel state changes"
        action={
          <Button variant="outline" size="sm" onClick={() => setShowLogs(true)}>
            Logs
          </Button>
        }
      />

      {events.length === 0 ? (
        <p className="text-sm text-muted-foreground">No activity yet.</p>
      ) : (
        <ul className="space-y-0 divide-y divide-border border-t border-border">
          {events.map((e) => (
            <li key={e.id} className="flex items-baseline justify-between gap-4 py-3 text-sm">
              <span>
                <span
                  className={cn(
                    'font-mono text-[10px] uppercase tracking-wider',
                    e.type === 'connected' && 'text-[hsl(var(--live))]',
                    e.type === 'failed' && 'text-destructive',
                    e.type !== 'connected' && e.type !== 'failed' && 'text-muted-foreground'
                  )}
                >
                  {e.type}
                </span>
                <span className="mx-2 text-muted-foreground">·</span>
                <span>{e.tunnelName}</span>
                <span className="ml-2 text-muted-foreground">{e.message}</span>
              </span>
              <time className="shrink-0 font-mono text-xs text-muted-foreground">
                {e.at.toLocaleTimeString()}
              </time>
            </li>
          ))}
        </ul>
      )}

      <LogPanel isOpen={showLogs} onClose={() => setShowLogs(false)} />
    </>
  )
}