import { useState } from 'react'
import { useForm, type SubmitHandler } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import * as z from 'zod'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Label } from './ui/label'
import { api } from '@/api/client'
import { useCreateTunnel, useUpdateTunnel } from '@/lib/queries'
import { Loader2 } from 'lucide-react'
import type { Tunnel, TunnelType } from '@/api/types'

const tunnelSchema = z.object({
  name: z.string().min(1, 'Name is required').max(50, 'Name too long'),
  type: z.enum(['local', 'remote', 'dynamic']),
  localPort: z.number().int().min(1).max(65535, 'Invalid port'),
  remoteHost: z.string().optional(),
  remotePort: z.number().int().min(1).max(65535, 'Invalid port').optional(),

  // SSH Connection (direct or via bastion)
  sshHost: z.string().min(1, 'SSH host is required'),
  sshPort: z.number().int().min(1).max(65535, 'Invalid port'),
  sshUser: z.string().min(1, 'Username is required'),
  identityFile: z.string().optional(),

  // Optional bastion host
  useBastionHost: z.boolean(),
  bastionHost: z.string().optional(),
  bastionPort: z.number().int().min(1).max(65535).optional(),
  bastionUser: z.string().optional(),
  bastionIdentityFile: z.string().optional(),

  autoReconnect: z.boolean(),
  agentId: z.string().optional(),
})

type TunnelFormData = z.infer<typeof tunnelSchema>

/** Maps an existing tunnel back into form fields for editing. */
function defaultsFromTunnel(tunnel?: Tunnel): TunnelFormData {
  const main = tunnel?.hops?.[tunnel.hops.length - 1]
  const bastion =
    tunnel && tunnel.hops.length > 1 ? tunnel.hops[0] : undefined

  return {
    name: tunnel?.name ?? '',
    type: (tunnel?.type as TunnelType) ?? 'local',
    localPort: tunnel?.localPort ?? 8080,
    remoteHost: tunnel?.remoteHost ?? '',
    remotePort: tunnel?.remotePort ?? 80,
    sshHost: main?.host ?? '',
    sshPort: main?.port ?? 22,
    sshUser: main?.user ?? '',
    identityFile: main?.key_id ?? '',
    useBastionHost: !!bastion,
    bastionHost: bastion?.host ?? '',
    bastionPort: bastion?.port ?? 22,
    bastionUser: bastion?.user ?? '',
    bastionIdentityFile: bastion?.key_id ?? '',
    autoReconnect: tunnel?.autoReconnect ?? true,
    agentId: tunnel?.agentId ?? '',
  }
}

interface TunnelFormProps {
  /** When provided, the form edits this tunnel instead of creating a new one. */
  tunnel?: Tunnel
  onSuccess: () => void
  onCancel: () => void
}

export function TunnelForm({ tunnel, onSuccess, onCancel }: TunnelFormProps) {
  const editing = !!tunnel
  const initial = defaultsFromTunnel(tunnel)
  const [tunnelType, setTunnelType] = useState<TunnelType>(initial.type)
  const [useBastionHost, setUseBastionHost] = useState(initial.useBastionHost)
  const [agentError, setAgentError] = useState<string | null>(null)

  const createTunnel = useCreateTunnel()
  const updateTunnel = useUpdateTunnel()
  const mutation = editing ? updateTunnel : createTunnel

  // Used to validate the optional agent field against agents that are online.
  const { data: agents = [] } = useQuery({
    queryKey: ['agents'],
    queryFn: () => api.listAgents(),
    refetchInterval: 10000,
  })

  const {
    register,
    handleSubmit,
    formState: { errors },
    reset,
  } = useForm<TunnelFormData>({
    resolver: zodResolver(tunnelSchema),
    defaultValues: initial,
  })

  const onSubmit: SubmitHandler<TunnelFormData> = async (data) => {
    // Validate the optional agent field: an agent name only works if a
    // matching data-plane agent is currently registered and online.
    const agentId = data.agentId?.trim()
    if (agentId && agentId !== 'local') {
      const online = agents.some((a) => a.id === agentId && a.status === 'online')
      if (!online) {
        setAgentError(
          `No agent named "${agentId}" is online. Leave this empty to run the ` +
            `tunnel on this server, or start the agent with: lazytunnel-agent --id ${agentId}`
        )
        return
      }
    }
    setAgentError(null)

    try {
      // Build hops array
      const hops = []

      // If using bastion, add bastion as first hop
      if (data.useBastionHost && data.bastionHost) {
        hops.push({
          host: data.bastionHost,
          port: data.bastionPort || 22,
          user: data.bastionUser || data.sshUser,
          auth_method: 'key' as const,
          key_id: data.bastionIdentityFile,
        })
      }

      // Add main SSH host
      hops.push({
        host: data.sshHost,
        port: data.sshPort,
        user: data.sshUser,
        auth_method: 'key' as const,
        key_id: data.identityFile,
      })

      const payload = {
        name: data.name,
        type: data.type,
        localPort: data.localPort,
        remoteHost: data.remoteHost || '',
        remotePort: data.remotePort || 0,
        hops,
        autoReconnect: data.autoReconnect,
        // The form has no inputs for these two. On edit, carry the tunnel's
        // existing values through rather than resetting them to the defaults.
        // The API emits and accepts seconds, so this round-trips safely.
        keepAlive: editing ? Math.round(tunnel!.keepAlive) : 30,
        maxRetries: editing ? tunnel!.maxRetries : 5,
        agentId: agentId || undefined,
      }

      if (editing) {
        await updateTunnel.mutateAsync({ id: tunnel!.id, data: payload })
      } else {
        await createTunnel.mutateAsync(payload)
      }

      reset()
      onSuccess()
    } catch (error) {
      alert(
        `Failed to ${editing ? 'update' : 'create'} tunnel: ` +
          (error instanceof Error ? error.message : 'Unknown error')
      )
    }
  }

  return (
    <form
      onSubmit={handleSubmit(onSubmit, (errs) => {
        alert(
          'Please fix the form errors:\n' +
            Object.entries(errs)
              .map(([field, err]) => `${field}: ${err?.message}`)
              .join('\n')
        )
      })}
      className="space-y-6"
    >
      {/* Basic Info */}
      <div className="space-y-4">
        <div className="space-y-2">
          <Label htmlFor="name">Tunnel Name</Label>
          <Input id="name" placeholder="prod-database" {...register('name')} />
          {errors.name && (
            <p className="text-sm text-destructive">{errors.name.message}</p>
          )}
        </div>

        <div className="space-y-2">
          <Label htmlFor="agentId">Agent (optional)</Label>
          <Input
            id="agentId"
            placeholder="hostname — leave empty for this server"
            className="font-mono text-sm"
            {...register('agentId')}
          />
          {agentError ? (
            <p className="text-xs text-destructive">{agentError}</p>
          ) : (
            <p className="text-xs text-muted-foreground">
              Leave empty to run on this server. To run on a remote host, start{' '}
              <span className="font-mono">lazytunnel-agent --id &lt;name&gt;</span>{' '}
              there first, then enter that name.
            </p>
          )}
        </div>

        <div className="space-y-2">
          <Label htmlFor="type">Tunnel Type</Label>
          <select
            id="type"
            className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            {...register('type')}
            onChange={(e) => setTunnelType(e.target.value as TunnelType)}
          >
            <option value="local">Local Forward</option>
            <option value="remote">Remote Forward</option>
            <option value="dynamic">Dynamic (SOCKS5)</option>
          </select>
          <p className="text-xs text-muted-foreground">
            {tunnelType === 'local' && 'Forward local port to remote destination'}
            {tunnelType === 'remote' && 'Forward remote port to local destination'}
            {tunnelType === 'dynamic' && 'Create SOCKS5 proxy'}
          </p>
        </div>
      </div>

      {/* Port Configuration */}
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <Label htmlFor="localPort">Local Port</Label>
          <Input
            id="localPort"
            type="number"
            {...register('localPort', { valueAsNumber: true })}
          />
          {errors.localPort && (
            <p className="text-sm text-destructive">{errors.localPort.message}</p>
          )}
        </div>

        {tunnelType !== 'dynamic' && (
          <div className="space-y-2">
            <Label htmlFor="remotePort">Remote Port</Label>
            <Input
              id="remotePort"
              type="number"
              {...register('remotePort', { valueAsNumber: true })}
            />
            {errors.remotePort && (
              <p className="text-sm text-destructive">{errors.remotePort.message}</p>
            )}
          </div>
        )}
      </div>

      {tunnelType !== 'dynamic' && (
        <div className="space-y-2">
          <Label htmlFor="remoteHost">Remote Host</Label>
          <Input
            id="remoteHost"
            placeholder="db.internal.example.com"
            {...register('remoteHost')}
          />
          {errors.remoteHost && (
            <p className="text-sm text-destructive">{errors.remoteHost.message}</p>
          )}
        </div>
      )}

      {/* SSH Connection */}
      <div className="space-y-4 rounded-lg border p-4">
        <h3 className="font-semibold">SSH Connection</h3>

        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-2">
            <Label htmlFor="sshHost">SSH Host</Label>
            <Input
              id="sshHost"
              placeholder="server.example.com"
              {...register('sshHost')}
            />
            {errors.sshHost && (
              <p className="text-sm text-destructive">{errors.sshHost.message}</p>
            )}
          </div>

          <div className="space-y-2">
            <Label htmlFor="sshPort">SSH Port</Label>
            <Input
              id="sshPort"
              type="number"
              {...register('sshPort', { valueAsNumber: true })}
            />
            {errors.sshPort && (
              <p className="text-sm text-destructive">{errors.sshPort.message}</p>
            )}
          </div>
        </div>

        <div className="space-y-2">
          <Label htmlFor="sshUser">Username</Label>
          <Input id="sshUser" placeholder="deploy" {...register('sshUser')} />
          {errors.sshUser && (
            <p className="text-sm text-destructive">{errors.sshUser.message}</p>
          )}
        </div>

        <div className="space-y-2">
          <Label htmlFor="identityFile">Identity File (Optional)</Label>
          <Input
            id="identityFile"
            placeholder="~/.ssh/id_rsa or leave empty for SSH agent"
            {...register('identityFile')}
          />
          <p className="text-xs text-muted-foreground">
            Path to SSH private key. Leave empty to use SSH agent.
          </p>
        </div>
      </div>

      {/* Optional Bastion Host */}
      <div className="space-y-4">
        <div className="flex items-center space-x-2">
          <input
            type="checkbox"
            id="useBastionHost"
            className="h-4 w-4 rounded border-gray-300"
            {...register('useBastionHost')}
            onChange={(e) => setUseBastionHost(e.target.checked)}
          />
          <Label htmlFor="useBastionHost" className="cursor-pointer">
            Use Bastion/Jump Host
          </Label>
        </div>

        {useBastionHost && (
          <div className="space-y-4 rounded-lg border p-4">
            <h3 className="font-semibold">Bastion Host Configuration</h3>

            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="bastionHost">Bastion Host</Label>
                <Input
                  id="bastionHost"
                  placeholder="bastion.example.com"
                  {...register('bastionHost')}
                />
                {errors.bastionHost && (
                  <p className="text-sm text-destructive">
                    {errors.bastionHost.message}
                  </p>
                )}
              </div>

              <div className="space-y-2">
                <Label htmlFor="bastionPort">SSH Port</Label>
                <Input
                  id="bastionPort"
                  type="number"
                  {...register('bastionPort', { valueAsNumber: true })}
                />
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="bastionUser">Username (Optional)</Label>
              <Input
                id="bastionUser"
                placeholder="Leave empty to use same as SSH user"
                {...register('bastionUser')}
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="bastionIdentityFile">Identity File (Optional)</Label>
              <Input
                id="bastionIdentityFile"
                placeholder="~/.ssh/bastion_key"
                {...register('bastionIdentityFile')}
              />
            </div>
          </div>
        )}
      </div>

      {/* Options */}
      <div className="flex items-center space-x-2">
        <input
          type="checkbox"
          id="autoReconnect"
          className="h-4 w-4 rounded border-gray-300"
          {...register('autoReconnect')}
        />
        <Label htmlFor="autoReconnect" className="cursor-pointer">
          Auto-reconnect on failure
        </Label>
      </div>

      {/* Actions */}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={mutation.isPending}>
          {mutation.isPending ? (
            <>
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              {editing ? 'Saving...' : 'Creating...'}
            </>
          ) : editing ? (
            'Save Changes'
          ) : (
            'Create Tunnel'
          )}
        </Button>
      </div>
    </form>
  )
}
