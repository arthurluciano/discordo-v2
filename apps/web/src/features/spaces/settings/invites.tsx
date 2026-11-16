// Server settings tabs: invites and bans (not in the store: fetched while the tab is open).
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { FieldError } from '@/components/ui/field'
import { Spinner } from '@/components/ui/spinner'
import { Avatar } from '@/design/avatar'
import { api } from '@/lib/api'
import { inviteLink } from '@/lib/pending-invite'
import { useCopy } from '@/lib/use-copy'
import type { Guild, Invite, User } from '@/lib/wire'
import { displayName } from '../../identity/users'

const DATE_TIME = new Intl.DateTimeFormat('pt-BR', {
  day: '2-digit',
  month: 'short',
  hour: '2-digit',
  minute: '2-digit',
})

const ListState = ({
  query,
  empty,
}: {
  query: { isPending: boolean; error: Error | null; data: unknown[] | undefined }
  empty: string
}) =>
  query.isPending ? (
    <div className="grid place-items-center py-8">
      <Spinner aria-label="Carregando" />
    </div>
  ) : query.error ? (
    <FieldError error={query.error} />
  ) : query.data?.length === 0 ? (
    <p className="py-8 text-center text-sm text-greyple">{empty}</p>
  ) : null

const InviteRow = ({ invite }: { invite: Invite }) => {
  const queryClient = useQueryClient()
  const revoke = useMutation({
    mutationFn: () => api.revokeInvite(invite.code),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['invites', invite.guild_id] }),
  })
  const { copied, copy } = useCopy()
  const creator = invite.creator ? displayName(invite.creator) : 'Desconhecido'

  return (
    <li className="space-y-1 rounded-lg border border-white/10 bg-surface-raised p-3">
      <div className="flex items-center gap-3">
        {invite.creator ? (
          <Avatar
            userId={invite.creator.id}
            displayName={creator}
            avatarUrl={invite.creator.avatar_url}
            size="small"
            ringClass="ring-surface-raised"
          />
        ) : null}
        <div className="min-w-0 flex-1">
          <p className="truncate font-mono text-sm text-blurple-soft">{invite.code}</p>
          <p className="truncate text-xs text-greyple">
            {creator} · {invite.uses}/{invite.max_uses || '∞'} usos ·{' '}
            {invite.expires_at ? `expira ${DATE_TIME.format(new Date(invite.expires_at))}` : 'não expira'}
          </p>
        </div>
        <Button
          size="icon-sm"
          variant="ghost"
          aria-label={`Copiar link do convite ${invite.code}`}
          title={copied ? 'Copiado' : 'Copiar link'}
          onClick={() => void copy(inviteLink(invite.code))}
        >
          {copied ? <Check aria-hidden /> : <Copy aria-hidden />}
        </Button>
        <Button
          size="icon-sm"
          variant="ghost"
          className="text-destructive hover:bg-destructive/15"
          aria-label={`Revogar convite ${invite.code}`}
          title="Revogar"
          disabled={revoke.isPending}
          onClick={() => revoke.mutate()}
        >
          {revoke.isPending ? <Spinner aria-hidden /> : <Trash2 aria-hidden />}
        </Button>
      </div>
      <FieldError error={revoke.error} />
    </li>
  )
}

export const Invites = ({ guild }: { guild: Guild }) => {
  const query = useQuery({ queryKey: ['invites', guild.id], queryFn: () => api.invites(guild.id) })

  return (
    <div className="space-y-3">
      <ListState query={query} empty="Nenhum convite ativo" />
      {query.data?.length ? (
        <ul className="max-h-[50vh] space-y-1 overflow-y-auto pr-1">
          {query.data.map((i) => (
            <InviteRow key={i.code} invite={i} />
          ))}
        </ul>
      ) : null}
    </div>
  )
}

const BanRow = ({ guildId, user }: { guildId: string; user: User }) => {
  const queryClient = useQueryClient()
  const unban = useMutation({
    mutationFn: () => api.unban(guildId, user.id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['bans', guildId] }),
  })
  const name = displayName(user)

  return (
    <li className="space-y-1 rounded-lg border border-white/10 bg-surface-raised p-3">
      <div className="flex items-center gap-3">
        <Avatar
          userId={user.id}
          displayName={name}
          avatarUrl={user.avatar_url}
          size="small"
          ringClass="ring-surface-raised"
        />
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium text-snow">{name}</p>
          <p className="truncate text-xs text-greyple">@{user.username}</p>
        </div>
        <Button size="xs" variant="secondary" onClick={() => unban.mutate()} loading={unban.isPending}>
          Desbanir
        </Button>
      </div>
      <FieldError error={unban.error} />
    </li>
  )
}

export const Bans = ({ guild }: { guild: Guild }) => {
  const query = useQuery({ queryKey: ['bans', guild.id], queryFn: () => api.bans(guild.id) })

  return (
    <div className="space-y-3">
      <ListState query={query} empty="Ninguém está banido" />
      {query.data?.length ? (
        <ul className="max-h-[50vh] space-y-1 overflow-y-auto pr-1">
          {query.data.map((b) => (
            <BanRow key={b.user.id} guildId={guild.id} user={b.user} />
          ))}
        </ul>
      ) : null}
    </div>
  )
}
