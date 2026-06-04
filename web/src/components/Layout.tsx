import { useState, type ReactNode } from 'react'
import { Menu, LogOut } from 'lucide-react'
import { Button } from './ui/button'
import { CreateTunnelDialog } from './CreateTunnelDialog'
import { ThemeToggle } from './ThemeToggle'
import { DemoModeToggle } from './DemoModeToggle'
import { SystemStatusBar } from './SystemStatusBar'
import { useAuthStore } from '@/store/authStore'
import { cn } from '@/lib/utils'

export type PageType = 'tunnels' | 'topology' | 'monitoring' | 'metrics' | 'settings'

const nav: { id: PageType; label: string }[] = [
  { id: 'tunnels', label: 'Tunnels' },
  { id: 'topology', label: 'Topology' },
  { id: 'monitoring', label: 'Activity' },
  { id: 'metrics', label: 'Metrics' },
  { id: 'settings', label: 'Settings' },
]

interface LayoutProps {
  children: ReactNode
  activePage?: PageType
  onPageChange?: (page: PageType) => void
}

export function Layout({ children, activePage = 'tunnels', onPageChange }: LayoutProps) {
  const [open, setOpen] = useState(false)
  const logout = useAuthStore((s) => s.logout)
  const authRequired = useAuthStore((s) => s.authRequired)

  return (
    <div className="min-h-screen">
      <header className="sticky top-0 z-40 border-b border-border bg-background/90 backdrop-blur-md">
        <div className="mx-auto flex h-14 max-w-5xl items-center gap-4 px-4 md:px-6">
          <Button
            variant="ghost"
            size="icon"
            className="md:hidden"
            onClick={() => setOpen(!open)}
          >
            <Menu className="h-4 w-4" />
          </Button>

          <span className="font-mono text-sm font-medium tracking-tight">lazytunnel</span>

          <nav className="hidden flex-1 gap-1 md:flex">
            {nav.map((item) => (
              <button
                key={item.id}
                onClick={() => onPageChange?.(item.id)}
                className={cn(
                  'rounded-md px-3 py-1.5 text-sm transition-colors',
                  activePage === item.id
                    ? 'bg-foreground text-background'
                    : 'text-muted-foreground hover:text-foreground'
                )}
              >
                {item.label}
              </button>
            ))}
          </nav>

          <div className="ml-auto flex items-center gap-1">
            <DemoModeToggle />
            <ThemeToggle />
            {authRequired && (
              <Button variant="ghost" size="icon" onClick={logout} title="Sign out">
                <LogOut className="h-4 w-4" />
              </Button>
            )}
            <CreateTunnelDialog />
          </div>
        </div>

        {open && (
          <nav className="flex flex-col gap-1 border-t border-border px-4 py-3 md:hidden">
            {nav.map((item) => (
              <button
                key={item.id}
                onClick={() => {
                  onPageChange?.(item.id)
                  setOpen(false)
                }}
                className={cn(
                  'rounded-md px-3 py-2 text-left text-sm',
                  activePage === item.id
                    ? 'bg-foreground text-background'
                    : 'text-muted-foreground'
                )}
              >
                {item.label}
              </button>
            ))}
          </nav>
        )}

        <div className="mx-auto max-w-5xl px-4 md:px-6">
          <SystemStatusBar />
        </div>
      </header>

      <main className="mx-auto max-w-5xl px-4 py-8 md:px-6 md:py-10">{children}</main>
    </div>
  )
}