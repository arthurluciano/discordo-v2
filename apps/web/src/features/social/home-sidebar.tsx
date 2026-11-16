import { useState } from 'react'
import { Plus, Users, Volume2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Avatar } from '@/design/avatar'
import { displayName, useMe, useOnline } from '@/features/identity/users'
import { api } from '@/lib/api'
import { useStore } from '@/lib/store'
import { cn } from '@/lib/utils'
import { ChannelType, RelationshipStatus, type DM, type User } from '@/lib/wire'
import { navigate } from '../shell/navigation'
import { FriendPicker } from './dialogs'
import { dmTitle, otherRecipients } from './dm'

const GROUP_MAX = 10

const PresenceAvatar = ({ user, ringClass }: { user: User; ringClass: string }) => {
  const online = useOnline(user.id)
  return <Avatar userId={user.id} displayName={displayName(user)} avatarUrl={user.avatar_url} state={online ? 'online' : 'offline'} ringClass={ringClass} />
}

const DMRow = ({ dm, active }: { dm: DM; active: boolean }) => {
  const me = useMe()
  const calling = useStore((s) => Object.values(s.voice).some((v) => v.channel_id === dm.id))
  const others = otherRecipients(dm, me?.id)
  const ring = active ? 'ring-surface-active' : 'ring-surface-sidebar'

  return (
    <button
      type="button"
      onClick={() => navigate.dm(dm.id)}
      aria-current={active ? 'page' : undefined}
      className={cn('flex h-11 w-full items-center gap-2.5 rounded-lg px-2 text-left transition-colors', active ? 'bg-surface-active text-snow' : 'text-greyple hover:bg-surface-hover hover:text-fog')}
    >
      {dm.type === ChannelType.DM && others[0] ? (
        <PresenceAvatar user={others[0]} ringClass={ring} />
      ) : (
        <span className="grid size-8 shrink-0 place-items-center rounded-full bg-blurple text-white"><Users className="size-4" aria-hidden /></span>
      )}
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium">{dmTitle(dm, me?.id)}</span>
        {dm.type === ChannelType.Group ? <span className="block truncate text-xs text-greyple">{dm.recipients.length} membros</span> : null}
      </span>
      {calling ? <Volume2 className="size-4 shrink-0 text-spring-green" aria-label="Chamada em andamento" /> : null}
    </button>
  )
}

export const HomeSidebar = ({ dmId }: { dmId: string | null }) => {
  const dms = useStore((s) => s.dms)
  const incoming = useStore((s) => s.relationships.filter((r) => r.status === RelationshipStatus.Incoming).length)
  const [creating, setCreating] = useState(false)

  return (
    <div className="flex min-h-0 flex-1 flex-col text-snow">
      <nav className="p-2" aria-label="Início">
        <button
          type="button"
          onClick={() => navigate.friends()}
          aria-current={dmId === null ? 'page' : undefined}
          className={cn('flex h-10 w-full items-center gap-3 rounded-lg px-3 text-sm font-semibold transition-colors', dmId === null ? 'bg-surface-active text-snow' : 'text-fog hover:bg-surface-hover hover:text-snow')}
        >
          <Users className="size-5" aria-hidden />
          <span className="flex-1 text-left">Amigos</span>
          {incoming ? <span className="grid h-5 min-w-5 place-items-center rounded-full bg-destructive px-1.5 text-xs font-bold text-white" aria-label={`${incoming} pedidos de amizade`}>{incoming}</span> : null}
        </button>
      </nav>

      <div className="flex items-center justify-between px-4 py-2">
        <h2 className="section-label">Mensagens diretas</h2>
        <Button type="button" variant="ghost" size="icon-xs" onClick={() => setCreating(true)} aria-label="Criar grupo"><Plus aria-hidden /></Button>
      </div>
      <ScrollArea className="min-h-0 flex-1 px-2">
        <div className="space-y-0.5 pb-3">
          {dms.map((dm) => <DMRow key={dm.id} dm={dm} active={dm.id === dmId} />)}
          {dms.length === 0 ? <p className="px-3 py-5 text-center text-xs text-greyple">Nenhuma conversa ainda.</p> : null}
        </div>
      </ScrollArea>

      <FriendPicker
        open={creating}
        onOpenChange={setCreating}
        title="Criar grupo"
        description="Escolha amigos para a conversa em grupo."
        submitLabel="Criar grupo"
        max={GROUP_MAX - 1}
        withName
        onSubmit={async (ids, name) => navigate.dm((await api.createGroup(ids, name || undefined)).id)}
      />
    </div>
  )
}
