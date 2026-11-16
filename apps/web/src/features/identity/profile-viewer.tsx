import { createContext, useContext, useMemo, useState, type ReactNode } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Check, MessageCircle, Pencil, UserPlus } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { FieldError } from '@/components/ui/field'
import { Avatar } from '@/design/avatar'
import { api } from '@/lib/api'
import { useStore } from '@/lib/store'
import { RelationshipStatus } from '@/lib/wire'
import { navigate } from '../shell/navigation'
import { EditProfileDialog } from './edit-profile-dialog'
import { displayName, useMe, useOnline, useUser } from './users'

type ProfileViewer = { openProfile: (userId: string) => void }

const ProfileViewerContext = createContext<ProfileViewer>({ openProfile: () => {} })

/** Opens anyone's profile card (avatar, name, message, friend request). */
export const useProfileViewer = () => useContext(ProfileViewerContext)

const FriendAction = ({ userId, username }: { userId: string; username: string }) => {
  const status = useStore((s) => s.relationships.find((r) => r.user.id === userId)?.status ?? RelationshipStatus.None)
  const change = useMutation({
    mutationFn: () => (status === RelationshipStatus.Incoming ? api.acceptFriend(userId) : api.addFriend(username)),
  })
  if (status === RelationshipStatus.Blocked) return null
  return (
    <>
      {status === RelationshipStatus.Friend ? <Badge variant="outline"><Check aria-hidden />Amigos</Badge>
        : status === RelationshipStatus.Outgoing ? <Badge variant="outline">Pedido enviado</Badge>
          : (
            <Button type="button" variant="secondary" size="sm" onClick={() => change.mutate()} disabled={change.isPending}>
              <UserPlus aria-hidden />{status === RelationshipStatus.Incoming ? 'Aceitar amizade' : 'Adicionar amigo'}
            </Button>
          )}
      <FieldError error={change.error} />
    </>
  )
}

const ProfileCard = ({ userId, onClose, onEdit }: { userId: string; onClose: () => void; onEdit: () => void }) => {
  const user = useUser(userId)
  const me = useMe()
  const online = useOnline(userId)
  const message = useMutation({
    mutationFn: () => api.openDM(userId),
    onSuccess: (dm) => {
      onClose()
      navigate.dm(dm.id)
    },
  })
  if (!user) return <p className="p-6 text-sm text-fog">Usuário indisponível.</p>
  const isSelf = me?.id === userId

  return (
    <div>
      <div className="h-24 bg-gradient-to-br from-blurple/60 to-fuchsia/40" />
      <div className="px-5 pb-5">
        <div className="-mt-10 mb-3 flex items-end justify-between">
          <span className="rounded-full bg-popover ring-4 ring-popover">
            <Avatar userId={user.id} displayName={displayName(user)} avatarUrl={user.avatar_url} size="large" state={online ? 'online' : 'offline'} ringClass="ring-popover" />
          </span>
          {isSelf ? <Button type="button" variant="secondary" size="sm" onClick={onEdit}><Pencil aria-hidden />Editar perfil</Button> : null}
        </div>
        <h2 className="text-lg font-bold leading-tight text-snow">{displayName(user)}</h2>
        <p className="text-sm font-medium text-greyple">@{user.username}</p>
        {isSelf ? null : (
          <div className="mt-4 flex flex-wrap items-center gap-2 border-t border-white/5 pt-4">
            <Button type="button" size="sm" onClick={() => message.mutate()} disabled={message.isPending}><MessageCircle aria-hidden />Mensagem</Button>
            <FriendAction userId={user.id} username={user.username} />
            <FieldError error={message.error} />
          </div>
        )}
      </div>
    </div>
  )
}

export const ProfileViewerProvider = ({ children }: { children: ReactNode }) => {
  const [userId, setUserId] = useState<string | null>(null)
  const [editing, setEditing] = useState(false)
  const value = useMemo<ProfileViewer>(() => ({ openProfile: setUserId }), [])

  return (
    <ProfileViewerContext.Provider value={value}>
      {children}
      <Dialog open={userId !== null} onOpenChange={(open) => !open && setUserId(null)}>
        <DialogContent className="max-w-sm gap-0 overflow-hidden p-0" showCloseButton={false}>
          <DialogHeader className="sr-only">
            <DialogTitle>Perfil</DialogTitle>
            <DialogDescription>Cartão de perfil.</DialogDescription>
          </DialogHeader>
          {userId ? <ProfileCard userId={userId} onClose={() => setUserId(null)} onEdit={() => { setUserId(null); setEditing(true) }} /> : null}
        </DialogContent>
      </Dialog>
      <EditProfileDialog open={editing} onOpenChange={setEditing} />
    </ProfileViewerContext.Provider>
  )
}
