// Server settings tab: members, their roles, kick and ban.
import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Ban, Crown, Plus, Search, UserMinus, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { FieldError } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Avatar } from '@/design/avatar'
import { api } from '@/lib/api'
import { has, Permission, topPosition } from '@/lib/permissions'
import type { Guild, Member, Role } from '@/lib/wire'
import { displayName } from '../../identity/users'
import { roleColor } from './roles'

const MemberRow = ({ guild, member, myTop, meId }: { guild: Guild; member: Member; myTop: number; meId: string }) => {
  const [confirming, setConfirming] = useState<'kick' | 'ban' | null>(null)
  const name = displayName(member.user, member.nickname)
  const isOwner = member.user.id === guild.owner_id
  const actionable = !isOwner && member.user.id !== meId && topPosition(guild, member.user.id) < myTop
  const canRoles = actionable && has(guild.permissions, Permission.ManageRoles)
  const assignable = (r: Role) => canRoles && r.id !== guild.id && r.position < myTop
  const held = guild.roles.filter((r) => member.roles.includes(r.id))
  const addable = guild.roles.filter((r) => assignable(r) && !member.roles.includes(r.id))

  const setRole = useMutation({
    mutationFn: ({ roleId, on }: { roleId: string; on: boolean }) =>
      api.setMemberRole(guild.id, member.user.id, roleId, on),
  })
  const punish = useMutation({
    mutationFn: (what: 'kick' | 'ban') => (what === 'kick' ? api.kick : api.ban)(guild.id, member.user.id),
    onSettled: () => setConfirming(null),
  })
  const error = setRole.error ?? punish.error

  return (
    <li className="space-y-2 rounded-lg border border-white/10 bg-surface-raised p-3">
      <div className="flex items-center gap-3">
        <Avatar
          userId={member.user.id}
          displayName={name}
          avatarUrl={member.user.avatar_url}
          size="small"
          ringClass="ring-surface-raised"
        />
        <div className="min-w-0 flex-1">
          <p className="flex items-center gap-1.5 truncate text-sm font-medium text-snow">
            {name}
            {isOwner ? <Crown aria-label="Dono do servidor" className="size-3.5 shrink-0 text-amber-400" /> : null}
          </p>
          <p className="truncate text-xs text-greyple">@{member.user.username}</p>
        </div>
        {actionable && confirming ? (
          <>
            <Button size="xs" variant="ghost" onClick={() => setConfirming(null)}>
              Cancelar
            </Button>
            <Button
              size="xs"
              variant="destructive"
              autoFocus
              onClick={() => punish.mutate(confirming)}
              loading={punish.isPending}
            >
              {confirming === 'kick' ? 'Expulsar' : 'Banir'} {name}
            </Button>
          </>
        ) : actionable ? (
          <>
            {has(guild.permissions, Permission.KickMembers) ? (
              <Button
                size="icon-sm"
                variant="ghost"
                className="text-destructive hover:bg-destructive/15"
                aria-label={`Expulsar ${name}`}
                title="Expulsar"
                onClick={() => setConfirming('kick')}
              >
                <UserMinus aria-hidden />
              </Button>
            ) : null}
            {has(guild.permissions, Permission.BanMembers) ? (
              <Button
                size="icon-sm"
                variant="ghost"
                className="text-destructive hover:bg-destructive/15"
                aria-label={`Banir ${name}`}
                title="Banir"
                onClick={() => setConfirming('ban')}
              >
                <Ban aria-hidden />
              </Button>
            ) : null}
          </>
        ) : null}
      </div>
      {held.length || addable.length ? (
        <div className="flex flex-wrap items-center gap-1">
          {held.map((r) => (
            <span
              key={r.id}
              className="inline-flex items-center gap-1 rounded-md border border-white/10 px-1.5 py-0.5 text-xs text-fog"
            >
              <span aria-hidden className="size-2 rounded-full bg-greyple" style={{ backgroundColor: roleColor(r) }} />
              {r.name}
              {assignable(r) ? (
                <button
                  type="button"
                  aria-label={`Tirar o cargo ${r.name}`}
                  className="text-greyple hover:text-snow"
                  disabled={setRole.isPending}
                  onClick={() => setRole.mutate({ roleId: r.id, on: false })}
                >
                  <X className="size-3" aria-hidden />
                </button>
              ) : null}
            </span>
          ))}
          {addable.length ? (
            <Select
              value=""
              onValueChange={(roleId) => setRole.mutate({ roleId, on: true })}
              disabled={setRole.isPending}
            >
              <SelectTrigger size="sm" className="h-6 w-auto gap-1 px-1.5 text-xs" aria-label={`Dar cargo a ${name}`}>
                <Plus className="size-3" aria-hidden />
                <SelectValue placeholder="Cargo" />
              </SelectTrigger>
              <SelectContent position="popper">
                {addable.map((r) => (
                  <SelectItem key={r.id} value={r.id}>
                    {r.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : null}
        </div>
      ) : null}
      <FieldError error={error} />
    </li>
  )
}

export const Members = ({ guild, myTop, meId }: { guild: Guild; myTop: number; meId: string }) => {
  const [search, setSearch] = useState('')
  const query = search.trim().toLowerCase()
  const filtered = guild.members.filter(
    (m) =>
      !query ||
      displayName(m.user, m.nickname).toLowerCase().includes(query) ||
      m.user.username.toLowerCase().includes(query),
  )

  return (
    <div className="space-y-3">
      <div className="relative">
        <Search className="absolute left-3 top-2.5 size-4 text-greyple" aria-hidden />
        <Input
          placeholder="Procurar membro..."
          aria-label="Procurar membro"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="pl-9"
        />
      </div>
      <p className="text-xs text-greyple">
        {guild.members.length} membro{guild.members.length === 1 ? '' : 's'}
      </p>
      <ul className="max-h-[50vh] space-y-1 overflow-y-auto pr-1">
        {filtered.length === 0 ? (
          <li className="py-8 text-center text-sm text-greyple">Nenhum resultado</li>
        ) : (
          filtered.map((m) => <MemberRow key={m.user.id} guild={guild} member={m} myTop={myTop} meId={meId} />)
        )}
      </ul>
    </div>
  )
}
