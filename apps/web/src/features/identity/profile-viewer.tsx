import { createContext, useContext, useMemo, useState, type ReactNode } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Check, MessageCircle, Pencil, UserPlus } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { FieldError } from '@/components/ui/field'
import { api } from '@/lib/api'
import { useStore } from '@/lib/store'
import { RelationshipStatus } from '@/lib/wire'
import { navigate } from '../shell/navigation'
import { ProfileCardView } from './profile-card'
import { useProfile, UserSettingsDialog, useSettingsState, type SettingsSection } from './user-settings-dialog'
import { useMe, useOnline, useUser } from './users'

type ProfileViewer = { openProfile: (userId: string) => void; openSettings: (section: SettingsSection) => void }

const ProfileViewerContext = createContext<ProfileViewer>({ openProfile: () => {}, openSettings: () => {} })

/** Opens anyone's profile card (avatar, name, message, friend request), or our own settings. */
export const useProfileViewer = () => useContext(ProfileViewerContext)

const FriendAction = ({ userId, username }: { userId: string; username: string }) => {
  const status = useStore((s) => s.relationships.find((r) => r.user.id === userId)?.status ?? RelationshipStatus.None)
  const change = useMutation({
    mutationFn: () => (status === RelationshipStatus.Incoming ? api.acceptFriend(userId) : api.addFriend(username)),
  })
  if (status === RelationshipStatus.Blocked) return null

  return (
    <>
      {status === RelationshipStatus.Friend ? (
        <Badge variant="outline">
          <Check aria-hidden />
          Amigos
        </Badge>
      ) : status === RelationshipStatus.Outgoing ? (
        <Badge variant="outline">Pedido enviado</Badge>
      ) : (
        <Button type="button" variant="secondary" size="sm" onClick={() => change.mutate()} disabled={change.isPending}>
          <UserPlus aria-hidden />
          {status === RelationshipStatus.Incoming ? 'Aceitar amizade' : 'Adicionar amigo'}
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
  const profile = useProfile(userId)

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
    <ProfileCardView
      // The live user wins over the fetched one: a USER_UPDATE renames the card at once.
      profile={{ ...profile.data, ...user }}
      online={online}
      corner={
        isSelf ? (
          <Button type="button" variant="secondary" size="sm" onClick={onEdit}>
            <Pencil aria-hidden />
            Editar perfil
          </Button>
        ) : null
      }
      actions={
        isSelf ? null : (
          <>
            <Button type="button" size="sm" onClick={() => message.mutate()} disabled={message.isPending}>
              <MessageCircle aria-hidden />
              Mensagem
            </Button>

            <FriendAction userId={user.id} username={user.username} />

            <FieldError error={message.error} />
          </>
        )
      }
    />
  )
}

export const ProfileViewerProvider = ({ children }: { children: ReactNode }) => {
  const [userId, setUserId] = useState<string | null>(null)
  const settings = useSettingsState()
  const { openAt } = settings

  const value = useMemo<ProfileViewer>(() => ({ openProfile: setUserId, openSettings: openAt }), [openAt])

  return (
    <ProfileViewerContext.Provider value={value}>
      {children}

      <Dialog open={userId !== null} onOpenChange={(open) => !open && setUserId(null)}>
        <DialogContent className="max-w-sm gap-0 overflow-hidden p-0" showCloseButton={false}>
          <DialogHeader className="sr-only">
            <DialogTitle>Perfil</DialogTitle>
            <DialogDescription>Cartão de perfil.</DialogDescription>
          </DialogHeader>

          {userId ? (
            <ProfileCard
              userId={userId}
              onClose={() => setUserId(null)}
              onEdit={() => {
                setUserId(null)
                openAt('profile')
              }}
            />
          ) : null}
        </DialogContent>
      </Dialog>

      <UserSettingsDialog
        open={settings.open}
        section={settings.section}
        onSectionChange={settings.setSection}
        onOpenChange={settings.setOpen}
      />
    </ProfileViewerContext.Provider>
  )
}
