import { useState } from 'react'
import { useForm, type SubmitHandler } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import * as z from 'zod'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from './ui/dialog'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Label } from './ui/label'
import { useCreateTunnel } from '@/lib/queries'
import { Plus, Loader2 } from 'lucide-react'
import type { TunnelType } from '@/types/tunnel'

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
})

type TunnelFormData = z.infer<typeof tunnelSchema>

export function CreateTunnelDialog() {
  const [open, setOpen] = useState(false)
  const [tunnelType, setTunnelType] = useState<TunnelType>('local')
  const [useBastionHost, setUseBastionHost] = useState(false)
  const createTunnel = useCreateTunnel()

  const handleOpenChange = (newOpen: boolean) => {
    console.log('🚇 Create Tunnel Dialog:', newOpen ? 'OPENING' : 'CLOSING')
    setOpen(newOpen)
  }

  const {
    register,
    handleSubmit,
    formState: { errors },
    reset,
  } = useForm<TunnelFormData>({
    resolver: zodResolver(tunnelSchema),
    defaultValues: {
      type: 'local' as const,
      localPort: 8080,
      remotePort: 80,
      sshPort: 22,
      bastionPort: 22,
      autoReconnect: true,
      useBastionHost: false,
    },
  })

  const onSubmit: SubmitHandler<TunnelFormData> = async (data) => {
    console.log('📝 Form submitted with data:', data)

    try {
      // Build hops array
      const hops = []

      // If using bastion, add bastion as first hop
      if (data.useBastionHost && data.bastionHost) {
        console.log('🔗 Adding bastion hop:', data.bastionHost)
        hops.push({
          host: data.bastionHost,
          port: data.bastionPort || 22,
          user: data.bastionUser || data.sshUser,
          auth_method: 'key' as const,  // Note: snake_case for backend
          key_id: data.bastionIdentityFile,  // Note: snake_case for backend
        })
      }

      // Add main SSH host
      console.log('🔗 Adding main SSH hop:', data.sshHost)
      hops.push({
        host: data.sshHost,
        port: data.sshPort,
        user: data.sshUser,
        auth_method: 'key' as const,  // Note: snake_case for backend
        key_id: data.identityFile,  // Note: snake_case for backend
      })

      const payload = {
        name: data.name,
        type: data.type,
        localPort: data.localPort,
        remoteHost: data.remoteHost || '',
        remotePort: data.remotePort || 0,
        hops,
        autoReconnect: data.autoReconnect,
        keepAlive: 30,
        maxRetries: 5,
      }

      console.log('🚀 Sending create tunnel request:', payload)

      const result = await createTunnel.mutateAsync(payload)

      console.log('✅ Tunnel created successfully:', result)
      setOpen(false)
      reset()
    } catch (error) {
      console.error('❌ Failed to create tunnel:', error)
      alert(`Failed to create tunnel: ${error instanceof Error ? error.message : 'Unknown error'}`)
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger asChild>
        <Button
          className="gap-2"
          onClick={() => console.log('🖱️ New Tunnel button clicked')}
        >
          <Plus className="h-4 w-4" />
          New Tunnel
        </Button>
      </DialogTrigger>
      <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Create New Tunnel</DialogTitle>
          <DialogDescription>
            Configure a new SSH tunnel with port forwarding
          </DialogDescription>
        </DialogHeader>

        <form
          onSubmit={(e) => {
            console.log('📋 Form submit event triggered')
            console.log('🔍 Current form errors:', errors)
            console.log('🔍 Has errors?', Object.keys(errors).length > 0)
            handleSubmit(
              (data) => {
                console.log('✅ Form validation passed!')
                onSubmit(data)
              },
              (errors) => {
                console.log('❌ Form validation FAILED:', errors)
                alert('Please fix the form errors:\n' + Object.entries(errors).map(([field, err]) => `${field}: ${err?.message}`).join('\n'))
              }
            )(e)
          }}
          className="space-y-6"
        >
          {/* Basic Info */}
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="name">Tunnel Name</Label>
              <Input
                id="name"
                placeholder="prod-database"
                {...register('name')}
              />
              {errors.name && (
                <p className="text-sm text-destructive">{errors.name.message}</p>
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
              <Input
                id="sshUser"
                placeholder="deploy"
                {...register('sshUser')}
              />
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
                      <p className="text-sm text-destructive">{errors.bastionHost.message}</p>
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
            <Button
              type="button"
              variant="outline"
              onClick={() => setOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={createTunnel.isPending}
              onClick={() => console.log('🔘 Submit button clicked!')}
            >
              {createTunnel.isPending ? (
                <>
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  Creating...
                </>
              ) : (
                'Create Tunnel'
              )}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
