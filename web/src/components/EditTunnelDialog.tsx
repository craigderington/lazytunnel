import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from './ui/dialog'
import { TunnelForm } from './TunnelForm'
import type { Tunnel } from '@/api/types'

interface EditTunnelDialogProps {
  tunnel: Tunnel
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function EditTunnelDialog({
  tunnel,
  open,
  onOpenChange,
}: EditTunnelDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Edit Tunnel</DialogTitle>
          <DialogDescription>
            Update the configuration for{' '}
            <span className="font-medium">{tunnel.name}</span>
          </DialogDescription>
        </DialogHeader>

        {open && (
          <TunnelForm
            tunnel={tunnel}
            onSuccess={() => onOpenChange(false)}
            onCancel={() => onOpenChange(false)}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}
