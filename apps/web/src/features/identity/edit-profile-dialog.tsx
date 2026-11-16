import { useState, type FormEvent } from 'react'
import { useMutation } from '@tanstack/react-query'
import { ImageUp, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { Avatar } from '@/design/avatar'
import { api } from '@/lib/api'
import type { User } from '@/lib/wire'
import { useMe } from './users'

type EditProfileDialogProps = { open: boolean; onOpenChange: (open: boolean) => void }

/** Name and avatar. Saved ones win over Discord's from then on (logging in again keeps them). */
export const EditProfileDialog = ({ open, onOpenChange }: EditProfileDialogProps) => {
  const me = useMe()
  if (!me) return null
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Editar perfil</DialogTitle>
          <DialogDescription>Como as pessoas veem você no Discordo.</DialogDescription>
        </DialogHeader>
        {/* Mounted with the dialog: it starts from the saved profile each time, and later updates never clobber typing. */}
        <ProfileForm me={me} onDone={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  )
}

const ProfileForm = ({ me, onDone }: { me: User; onDone: () => void }) => {
  const [name, setName] = useState(me.display_name)
  const [avatar, setAvatar] = useState(me.avatar_url)
  const upload = useMutation({ mutationFn: api.uploadImage, onSuccess: setAvatar })
  const save = useMutation({
    mutationFn: () => api.updateProfile({ display_name: name, avatar_url: avatar }),
    onSuccess: onDone,
  })
  const submit = (event: FormEvent) => {
    event.preventDefault()
    save.mutate()
  }
  const failure = upload.error ?? save.error

  return (
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
        <FieldError error={failure} />
      </FieldGroup>
      <DialogFooter className="mt-6">
        <Button type="button" variant="ghost" onClick={onDone}>Cancelar</Button>
        <Button type="submit" disabled={upload.isPending} loading={save.isPending}>Salvar</Button>
      </DialogFooter>
    </form>
  )
}
