import { useState } from 'react'
import { Sparkles, X } from 'lucide-react'
import { Button } from './ui/button'
import { Badge } from './ui/badge'
import { useTunnelStore } from '@/store/tunnelStore'
import { DEMO_TUNNELS } from '@/lib/demoData'

export function DemoModeToggle() {
  const [demoMode, setDemoMode] = useState(false)
  const setTunnels = useTunnelStore((state) => state.setTunnels)
  const setStoreDemoMode = useTunnelStore((state) => state.setDemoMode)

  const toggleDemoMode = () => {
    console.log('🎭 Toggle demo mode clicked, current state:', demoMode)

    if (!demoMode) {
      // Enable demo mode
      console.log('✅ Enabling demo mode with', DEMO_TUNNELS.length, 'tunnels')
      console.table(DEMO_TUNNELS.map(t => ({
        name: t.name,
        type: t.type,
        status: t.status,
        port: t.localPort
      })))

      setStoreDemoMode(true) // Prevent API from overwriting
      setTunnels(DEMO_TUNNELS)
      setDemoMode(true)

      console.log('✨ Demo mode enabled! Check the tunnel list.')
    } else {
      // Disable demo mode
      console.log('❌ Disabling demo mode')
      setStoreDemoMode(false) // Re-enable API fetching
      setTunnels([])
      setDemoMode(false)

      console.log('✅ Demo mode disabled. Returning to live data.')
    }
  }

  return (
    <div className="flex items-center gap-2">
      <Button
        variant={demoMode ? 'default' : 'outline'}
        size="sm"
        onClick={toggleDemoMode}
        className="gap-2"
      >
        {demoMode ? (
          <>
            <X className="h-4 w-4" />
            Exit Demo
          </>
        ) : (
          <>
            <Sparkles className="h-4 w-4" />
            Demo Mode
          </>
        )}
      </Button>
      {demoMode && (
        <Badge variant="secondary" className="animate-pulse">
          Demo Active
        </Badge>
      )}
    </div>
  )
}
