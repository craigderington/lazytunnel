import { useState } from 'react'
import { Loader2 } from 'lucide-react'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Label } from './ui/label'
import { useAuthStore } from '@/store/authStore'
import { useTunnelStore } from '@/store/tunnelStore'
import { DEMO_TUNNELS } from '@/lib/demoData'

export function LoginPage() {
  const login = useAuthStore((s) => s.login)
  const error = useAuthStore((s) => s.error)
  const setTunnels = useTunnelStore((s) => s.setTunnels)
  const setDemoMode = useTunnelStore((s) => s.setDemoMode)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setSubmitting(true)
    try {
      await login(username, password)
    } catch {
      /* error in store */
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center px-4">
      <div className="w-full max-w-sm">
        <div className="mb-10">
          <p className="font-mono text-[11px] uppercase tracking-[0.2em] text-muted-foreground">
            SSH tunnel manager
          </p>
          <h1 className="mt-2 text-2xl font-medium tracking-tight">lazytunnel</h1>
        </div>

        <form onSubmit={handleSubmit} className="space-y-5">
          <div className="space-y-1.5">
            <Label htmlFor="username" className="text-xs text-muted-foreground">
              Username
            </Label>
            <Input
              id="username"
              autoComplete="username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              className="h-10 border-border/80 bg-transparent font-mono"
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="password" className="text-xs text-muted-foreground">
              Password
            </Label>
            <Input
              id="password"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="h-10 border-border/80 bg-transparent font-mono"
            />
          </div>

          {error && (
            <p className="text-sm text-destructive">{error}</p>
          )}

          <Button type="submit" className="h-10 w-full" disabled={submitting}>
            {submitting ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              'Sign in'
            )}
          </Button>
        </form>

        <button
          type="button"
          className="mt-8 w-full text-left text-sm text-muted-foreground transition-colors hover:text-foreground"
          onClick={() => {
            setDemoMode(true)
            setTunnels(DEMO_TUNNELS)
          }}
        >
          View demo →
        </button>
      </div>
    </div>
  )
}