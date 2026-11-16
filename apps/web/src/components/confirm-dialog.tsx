import { useMutation } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { FieldError } from '@/components/ui/field'

/** Asks before something destructive; stays open, showing the error, when it fails. */
type ConfirmDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description: string
  confirmLabel: string
  onConfirm: () => Promise<unknown>
}

export const ConfirmDialog = ({ open, onOpenChange, title, description, confirmLabel, onConfirm }: ConfirmDialogProps) => {
  const run = useMutation({ mutationFn: onConfirm, onSuccess: () => onOpenChange(false) })
  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next) run.reset(); onOpenChange(next) }}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <FieldError error={run.error} />
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>Cancelar</Button>
          <Button type="button" variant="destructive" onClick={() => run.mutate()} loading={run.isPending}>
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
