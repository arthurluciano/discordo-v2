import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Spinner } from '@/components/ui/spinner'
import { Avatar } from '@/design/avatar'
import { displayName } from '@/features/identity/users'
import { errorMessage } from '@/lib/api'
import { useStore } from '@/lib/store'
import { RelationshipStatus } from '@/lib/wire'

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
        {run.error ? <FieldError>{errorMessage(run.error)}</FieldError> : null}
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>Cancelar</Button>
          <Button type="button" variant="destructive" disabled={run.isPending} onClick={() => run.mutate()}>
            {run.isPending ? <Spinner aria-hidden /> : null}{confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

type FriendPickerProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description: string
  submitLabel: string
  /** Friends not to offer (already in the group). */
  exclude?: string[]
  max: number
  /** Also ask for an optional group name. */
  withName?: boolean
  onSubmit: (userIds: string[], name: string) => Promise<unknown>
}

/** Picks friends for a new group or to add to one. */
export const FriendPicker = ({ open, onOpenChange, title, description, submitLabel, exclude, max, withName, onSubmit }: FriendPickerProps) => {
  const relationships = useStore((s) => s.relationships)
  const friends = useMemo(
    () => relationships
      .filter((r) => r.status === RelationshipStatus.Friend && !exclude?.includes(r.user.id))
      .map((r) => r.user)
      .sort((a, b) => displayName(a).localeCompare(displayName(b), 'pt-BR')),
    [relationships, exclude],
  )
  const [picked, setPicked] = useState<string[]>([])
  const [name, setName] = useState('')
  const submit = useMutation({ mutationFn: () => onSubmit(picked, name.trim()), onSuccess: () => onOpenChange(false) })
  const { reset } = submit

  useEffect(() => {
    if (!open) return
    setPicked([])
    setName('')
    reset()
  }, [open, reset])

  const toggle = (id: string) => setPicked((p) => (p.includes(id) ? p.filter((x) => x !== id) : [...p, id]))
  const onFormSubmit = (event: FormEvent) => {
    event.preventDefault()
    if (picked.length) submit.mutate()
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description} ({picked.length}/{max})</DialogDescription>
        </DialogHeader>
        <form onSubmit={onFormSubmit} className="flex flex-col gap-4">
          {friends.length === 0 ? (
            <p className="py-6 text-center text-sm text-greyple">Nenhum amigo disponível.</p>
          ) : (
            <ScrollArea className="max-h-72">
              <ul className="space-y-0.5 pr-2">
                {friends.map((u) => {
                  const checked = picked.includes(u.id)
                  return (
                    <li key={u.id}>
                      <label className="flex cursor-pointer items-center gap-3 rounded-lg px-2 py-1.5 hover:bg-surface-hover has-disabled:cursor-not-allowed has-disabled:opacity-50">
                        <Avatar userId={u.id} displayName={displayName(u)} avatarUrl={u.avatar_url} ringClass="ring-popover" />
                        <span className="min-w-0 flex-1 truncate text-sm font-medium text-snow">{displayName(u)}</span>
                        <input type="checkbox" className="size-4 accent-blurple" checked={checked} disabled={!checked && picked.length >= max} onChange={() => toggle(u.id)} />
                      </label>
                    </li>
                  )
                })}
              </ul>
            </ScrollArea>
          )}
          {withName ? (
            <Field>
              <FieldLabel htmlFor="group-name" className="section-label">Nome do grupo (opcional)</FieldLabel>
              <Input id="group-name" maxLength={100} value={name} onChange={(event) => setName(event.target.value)} />
            </Field>
          ) : null}
          {submit.error ? <FieldError>{errorMessage(submit.error)}</FieldError> : null}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>Cancelar</Button>
            <Button type="submit" disabled={!picked.length || submit.isPending}>
              {submit.isPending ? <Spinner aria-hidden /> : null}{submitLabel}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
