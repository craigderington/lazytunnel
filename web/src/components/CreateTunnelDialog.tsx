import { useState } from 'react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from './ui/dialog'
import { Button } from './ui/button'
import { TunnelForm } from './TunnelForm'
import { Plus } from 'lucide-react'

export function CreateTunnelDialog() {
  const [open, setOpen] = useState(false)

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button className="gap-2">
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

        {open && (
          <TunnelForm
            onSuccess={() => setOpen(false)}
            onCancel={() => setOpen(false)}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}
