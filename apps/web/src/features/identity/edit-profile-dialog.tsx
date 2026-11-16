import { useEffect, useState, type FormEvent } from 'react'
import { useMutation } from '@tanstack/react-query'
import { ImageUp, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { Avatar } from '@/design/avatar'
import { api, errorMessage } from '@/lib/api'
import { useMe } from './users'

type EditProfileDialogProps = { open: boolean; onOpenChange: (open: boolean) => void }

/** Name and avatar. Saved ones win over Discord's from then on (logging in again keeps them). */
export const EditProfileDialog = ({ open, onOpenChange }: EditProfileDialogProps) => {
  const me = useMe()
  const [name, setName] = useState('')
  const [avatar, setAvatar] = useState('')

  useEffect(() => {
    if (open && me) {
      setName(me.display_name)
      setAvatar(me.avatar_url)
    }
  }, [open, me])

  const upload = useMutation({ mutationFn: api.uploadImage, onSuccess: setAvatar })
  const save = useMutation({
    mutationFn: () => api.updateProfile({ display_name: name, avatar_url: avatar }),
    onSuccess: () => onOpenChange(false),
  })

  if (!me) return null
  const submit = (event: FormEvent) => {
    event.preventDefault()
    save.mutate()
  }
  const failure = upload.error ?? save.error

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Editar perfil</DialogTitle>
          <DialogDescription>Como as pessoas veem você no Discordo.</DialogDescription>
        </DialogHeader>
        <form onSubmit={submit}>
          <FieldGroup>
            <div className="flex items-center gap-4">
              <Avatar userId={me.id} displayName={name || me.username} avatarUrl={avatar} size="large" />
              <div className="flex flex-col gap-2">
                <Button type="button" variant="secondary" size="sm" asChild disabled={upload.isPending}>
                  <label className="cursor-pointer">
                    {upload.isPending ? <Spinner aria-hidden /> : <ImageUp aria-hidden />}
                    Trocar avatar
                    <input
                      type="file" accept="image/png,image/jpeg,image/gif,image/webp" className="sr-only"
                      onChange={(event) => {
                        const file = event.target.files?.[0]
                        if (file) upload.mutate(file)
                        event.target.value = ''
                      }}
                    />
                  </label>
                </Button>
                {avatar ? (
                  <Button type="button" variant="ghost" size="sm" onClick={() => setAvatar('')}><Trash2 aria-hidden />Remover avatar</Button>
                ) : null}
              </div>
            </div>
            <Field>
              <FieldLabel htmlFor="profile-name" className="section-label">Nome de exibição</FieldLabel>
              <Input id="profile-name" required maxLength={32} value={name} onChange={(event) => setName(event.target.value)} />
            </Field>
            <p className="text-xs text-greyple">Usuário: @{me.username}</p>
            {failure ? <FieldError>{errorMessage(failure)}</FieldError> : null}
          </FieldGroup>
          <DialogFooter className="mt-6">
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>Cancelar</Button>
            <Button type="submit" disabled={save.isPending || upload.isPending}>{save.isPending ? <Spinner aria-hidden /> : null}Salvar</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
