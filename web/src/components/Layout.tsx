import { useState, type ReactNode } from 'react'
import {
  Network,
  Settings,
  Activity,
  Database,
  Menu,
  X
} from 'lucide-react'
import { Button } from './ui/button'
import { CreateTunnelDialog } from './CreateTunnelDialog'
import { ThemeToggle } from './ThemeToggle'
import { DemoModeToggle } from './DemoModeToggle'
import { cn } from '@/lib/utils'

interface LayoutProps {
  children: ReactNode
}

export function Layout({ children }: LayoutProps) {
  const [sidebarOpen, setSidebarOpen] = useState(true)

  return (
    <div className="min-h-screen bg-background">
      {/* Backdrop overlay for mobile */}
      {sidebarOpen && (
        <div
          className="fixed inset-0 z-30 bg-black/50 md:hidden"
          onClick={() => {
            console.log('🖱️ Backdrop clicked, closing sidebar')
            setSidebarOpen(false)
          }}
        />
      )}

      {/* Sidebar */}
      <aside
        className={cn(
          "fixed left-0 top-0 z-40 h-screen transition-all duration-300",
          "bg-card border-r border-border",
          sidebarOpen ? "w-64 translate-x-0" : "w-0 -translate-x-full"
        )}
      >
        <div className={cn(
          "flex h-full flex-col overflow-hidden",
          !sidebarOpen && "opacity-0"
        )}>
          {/* Logo & Toggle */}
          <div className="flex h-16 items-center justify-between px-6 border-b border-border flex-shrink-0">
            <button
              onClick={() => {
                console.log('🔄 Refreshing page')
                window.location.reload()
              }}
              className="flex items-center gap-2 overflow-hidden hover:opacity-80 transition-opacity cursor-pointer"
            >
              <Network className="h-6 w-6 text-primary flex-shrink-0" />
              <span className="text-xl font-bold whitespace-nowrap">lazytunnel</span>
            </button>
            <Button
              variant="ghost"
              size="icon"
              onClick={() => {
                console.log('🔽 Closing sidebar from X button')
                setSidebarOpen(false)
              }}
              className="md:hidden flex-shrink-0"
            >
              <X className="h-5 w-5" />
            </Button>
          </div>

          {/* Navigation */}
          <nav className="flex-1 space-y-1 p-4">
            <NavItem icon={<Network />} label="Tunnels" active />
            <NavItem icon={<Activity />} label="Monitoring" />
            <NavItem icon={<Database />} label="Metrics" />
            <NavItem icon={<Settings />} label="Settings" />
          </nav>

          {/* Footer */}
          <div className="border-t border-border p-4">
            <div className="text-sm text-muted-foreground">
              <div>lazytunnel v1.0.0</div>
              <div className="text-xs mt-1">Production SSH Manager</div>
            </div>
          </div>
        </div>
      </aside>

      {/* Main Content */}
      <div
        className={cn(
          "transition-all duration-300",
          sidebarOpen ? "ml-64" : "ml-0"
        )}
      >
        {/* Header */}
        <header className="sticky top-0 z-30 flex h-16 items-center gap-4 border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60 px-6">
          <Button
            variant="ghost"
            size="icon"
            onClick={() => {
              console.log('🍔 Hamburger clicked, toggling sidebar from', sidebarOpen, 'to', !sidebarOpen)
              setSidebarOpen(!sidebarOpen)
            }}
            title={sidebarOpen ? "Close sidebar" : "Open sidebar"}
          >
            <Menu className="h-5 w-5" />
          </Button>

          <div className="flex flex-1 items-center justify-between">
            <h1 className="text-2xl font-semibold">Tunnel Manager</h1>

            <div className="flex items-center gap-3">
              <DemoModeToggle />
              <ThemeToggle />
              <CreateTunnelDialog />
            </div>
          </div>
        </header>

        {/* Page Content */}
        <main className="container mx-auto p-6">
          {children}
        </main>
      </div>
    </div>
  )
}

interface NavItemProps {
  icon: ReactNode
  label: string
  active?: boolean
}

function NavItem({ icon, label, active }: NavItemProps) {
  const handleClick = () => {
    if (!active) {
      alert(`${label} - Coming soon! This feature is under development.`)
    }
  }

  return (
    <button
      onClick={handleClick}
      className={cn(
        "flex w-full items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
        active
          ? "bg-primary text-primary-foreground"
          : "text-muted-foreground hover:bg-accent hover:text-accent-foreground"
      )}
    >
      <span className="h-5 w-5">{icon}</span>
      {label}
    </button>
  )
}
