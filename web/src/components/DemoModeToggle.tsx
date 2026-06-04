import { useTunnelStore } from '@/store/tunnelStore'
import { DEMO_TUNNELS } from '@/lib/demoData'
import { Button } from './ui/button'
import { cn } from '@/lib/utils'

export function DemoModeToggle() {
  const isDemoMode = useTunnelStore((s) => s.isDemoMode)
  const setDemoMode = useTunnelStore((s) => s.setDemoMode)
  const setTunnels = useTunnelStore((s) => s.setTunnels)

  const toggle = () => {
    if (!isDemoMode) {
      setDemoMode(true)
      setTunnels(DEMO_TUNNELS)
    } else {
      setDemoMode(false)
      setTunnels([])
    }
  }

  return (
    <Button
      variant="ghost"
      size="sm"
      onClick={toggle}
      className={cn(
        'font-mono text-xs',
        isDemoMode && 'text-foreground'
      )}
    >
      {isDemoMode ? 'Exit demo' : 'Demo'}
    </Button>
  )
}