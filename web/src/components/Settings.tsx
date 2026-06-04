import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/api/client'
import { useSettingsStore } from '@/store/settingsStore'
import { useThemeStore } from '@/store/themeStore'
import { useTunnelStore } from '@/store/tunnelStore'
import { PageHeader } from './PageHeader'
import { Label } from './ui/label'
import { Input } from './ui/input'
import { Switch } from './ui/switch'
import { Button } from './ui/button'

export function Settings() {
  const { settings, updateSettings, resetSettings } = useSettingsStore()
  const { theme, toggleTheme } = useThemeStore()
  const { isDemoMode, setDemoMode } = useTunnelStore()
  const [local, setLocal] = useState(settings)
  const [dirty, setDirty] = useState(false)

  const { data: agents = [] } = useQuery({
    queryKey: ['agents'],
    queryFn: () => api.listAgents(),
    refetchInterval: 10000,
  })

  const set = <K extends keyof typeof settings>(key: K, value: (typeof settings)[K]) => {
    setLocal((p) => ({ ...p, [key]: value }))
    setDirty(true)
  }

  return (
    <>
      <PageHeader
        title="Settings"
        description="Preferences and defaults"
        action={
          dirty && (
            <Button
              size="sm"
              onClick={() => {
                updateSettings(local)
                setDirty(false)
              }}
            >
              Save
            </Button>
          )
        }
      />

      <section className="mb-10 space-y-3">
        <p className="text-xs uppercase tracking-wider text-muted-foreground">Agents</p>
        {agents.length === 0 ? (
          <p className="text-sm text-muted-foreground">No agents registered.</p>
        ) : (
          <ul className="divide-y divide-border border-t border-border font-mono text-sm">
            {agents.map((a) => (
              <li key={a.id} className="flex justify-between py-2">
                <span>{a.id}</span>
                <span
                  className={
                    a.status === 'online'
                      ? 'text-[hsl(var(--live))]'
                      : 'text-muted-foreground'
                  }
                >
                  {a.status}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="space-y-6 border-t border-border pt-8">
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">API base URL</Label>
          <Input
            value={local.apiBaseUrl}
            onChange={(e) => set('apiBaseUrl', e.target.value)}
            className="font-mono text-sm"
          />
        </div>

        <div className="flex items-center justify-between">
          <div>
            <p className="text-sm">Theme</p>
            <p className="text-xs text-muted-foreground">{theme}</p>
          </div>
          <Button variant="outline" size="sm" onClick={toggleTheme}>
            Toggle
          </Button>
        </div>

        <div className="flex items-center justify-between">
          <div>
            <p className="text-sm">Demo mode</p>
            <p className="text-xs text-muted-foreground">Sample data only</p>
          </div>
          <Switch checked={isDemoMode} onCheckedChange={setDemoMode} />
        </div>

        <div className="flex items-center justify-between">
          <div>
            <p className="text-sm">Auto-reconnect</p>
            <p className="text-xs text-muted-foreground">Default for new tunnels</p>
          </div>
          <Switch
            checked={local.defaultAutoReconnect}
            onCheckedChange={(v) => set('defaultAutoReconnect', v)}
          />
        </div>

        <Button
          variant="ghost"
          size="sm"
          className="text-muted-foreground"
          onClick={() => {
            resetSettings()
            setLocal(settings)
            setDirty(false)
          }}
        >
          Reset defaults
        </Button>
      </section>
    </>
  )
}