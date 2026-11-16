// Server settings: overview (name, icon, banner), roles, members, invites, bans, and deleting the server.
import { useRef, useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowDown, ArrowUp, Ban, Check, Copy, Crown, ImagePlus, Plus, Search, Trash2, UserMinus, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { FieldError } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Avatar } from '@/design/avatar'
import { api, errorMessage } from '@/lib/api'
import { has, Permission, topPosition } from '@/lib/permissions'
import { inviteLink } from '@/lib/pending-invite'
import { cn } from '@/lib/utils'
import type { Guild, Invite, Member, Role, User } from '@/lib/wire'
import { displayName, useMe } from '../identity/users'
import { permissionList } from './logic'
import { initialsOf } from './server-rail'

const toHex = (color: number) => `#${color.toString(16).padStart(6, '0')}`
const roleColor = (r: Role) => (r.color ? toHex(r.color) : undefined)

// ---- overview ----

const ImagePicker = ({ label, url, wide, fallback, onChange }: { label: string; url: string; wide?: boolean; fallback: string; onChange: (url: string) => void }) => {
  const input = useRef<HTMLInputElement>(null)
  const upload = useMutation({ mutationFn: (file: File) => api.uploadImage(file), onSuccess: onChange })
  return (
    <div className="space-y-2">
      <span className="section-label">{label}</span>
      <div className="flex items-center gap-3">
        <div className={cn('grid shrink-0 place-items-center overflow-hidden bg-surface-raised text-lg font-bold text-fog', wide ? 'h-20 w-40 rounded-lg' : 'size-20 rounded-2xl')}>
          {url ? <img src={url} alt="" className="size-full object-cover" /> : fallback}
        </div>
        <div className="flex flex-col gap-1.5">
          <Button type="button" size="sm" variant="secondary" onClick={() => input.current?.click()} disabled={upload.isPending}>
            {upload.isPending ? <Spinner aria-hidden /> : <ImagePlus aria-hidden />}Enviar imagem
          </Button>
          {url ? <Button type="button" size="sm" variant="ghost" onClick={() => onChange('')}><X aria-hidden />Remover</Button> : null}
        </div>
        <input ref={input} type="file" accept="image/png,image/jpeg,image/gif,image/webp" className="hidden" onChange={(e) => {
          const file = e.target.files?.[0]
          if (file) upload.mutate(file)
          e.target.value = ''
        }} />
      </div>
      {upload.error ? <FieldError>{errorMessage(upload.error)}</FieldError> : null}
    </div>
  )
}

const Overview = ({ guild }: { guild: Guild }) => {
  const [name, setName] = useState(guild.name)
  const [icon, setIcon] = useState(guild.icon_url)
  const [banner, setBanner] = useState(guild.banner_url)
  const dirty = name !== guild.name || icon !== guild.icon_url || banner !== guild.banner_url
  const save = useMutation({ mutationFn: () => api.updateGuild(guild.id, { name: name.trim(), icon_url: icon, banner_url: banner }) })
  const submit = (e: FormEvent) => {
    e.preventDefault()
    save.mutate()
  }

  return (
    <form onSubmit={submit} className="space-y-5">
      <div>
        <label htmlFor="guild-settings-name" className="section-label">Nome do servidor</label>
        <Input id="guild-settings-name" className="mt-2" maxLength={100} value={name} onChange={(e) => setName(e.target.value)} />
      </div>
      <ImagePicker label="Ícone" url={icon} fallback={initialsOf(name)} onChange={setIcon} />
      <ImagePicker label="Banner" url={banner} wide fallback="" onChange={setBanner} />
      {save.error ? <FieldError>{errorMessage(save.error)}</FieldError> : null}
      <Button type="submit" disabled={save.isPending || !dirty || !name.trim()}>{save.isPending ? <Spinner aria-hidden /> : null}Salvar alterações</Button>
    </form>
  )
}

// ---- roles ----

const RoleEditor = ({ guild, role, editable, above, below, onDeleted }: { guild: Guild; role: Role; editable: boolean; above: Role | undefined; below: Role | undefined; onDeleted: () => void }) => {
  const everyone = role.id === guild.id
  const [name, setName] = useState(role.name)
  const [color, setColor] = useState(role.color)
  const [confirming, setConfirming] = useState(false)
  const save = useMutation({ mutationFn: (input: Parameters<typeof api.updateRole>[2]) => api.updateRole(guild.id, role.id, input) })
  const move = useMutation({
    mutationFn: async (neighbor: Role) => {
      await api.moveRole(guild.id, role.id, neighbor === above)
    },
  })
  const remove = useMutation({ mutationFn: () => api.deleteRole(guild.id, role.id), onSuccess: onDeleted })
  const error = save.error ?? move.error ?? remove.error
  const submit = (e: FormEvent) => {
    e.preventDefault()
    save.mutate(everyone ? { color } : { name: name.trim(), color })
  }

  return (
    <div className="min-w-0 space-y-4">
      {!editable ? <p className="rounded-md bg-surface-raised px-3 py-2 text-xs text-greyple">Este cargo está na sua posição ou acima dela: você não pode editá-lo.</p> : null}
      <form onSubmit={submit} className="flex flex-wrap items-end gap-2">
        {everyone ? null : (
          <div className="min-w-40 flex-1">
            <label htmlFor="role-name" className="section-label">Nome</label>
            <Input id="role-name" className="mt-2" maxLength={100} value={name} disabled={!editable} onChange={(e) => setName(e.target.value)} />
          </div>
        )}
        <div>
          <label htmlFor="role-color" className="section-label">Cor</label>
          <div className="mt-2 flex items-center gap-1">
            <input id="role-color" type="color" value={toHex(color)} disabled={!editable} onChange={(e) => setColor(parseInt(e.target.value.slice(1), 16))} className="h-9 w-12 cursor-pointer rounded-md border border-white/10 bg-surface-raised disabled:opacity-45" />
            {color ? <Button type="button" size="icon-sm" variant="ghost" aria-label="Sem cor" title="Sem cor" disabled={!editable} onClick={() => setColor(0)}><X aria-hidden /></Button> : null}
          </div>
        </div>
        <Button type="submit" disabled={!editable || save.isPending || !name.trim() || (name === role.name && color === role.color)}>Salvar</Button>
      </form>

      {everyone ? null : (
        <div className="flex flex-wrap gap-2">
          <Button type="button" size="sm" variant="secondary" disabled={!editable || !above || move.isPending} onClick={() => above && move.mutate(above)}><ArrowUp aria-hidden />Subir</Button>
          <Button type="button" size="sm" variant="secondary" disabled={!editable || !below || move.isPending} onClick={() => below && move.mutate(below)}><ArrowDown aria-hidden />Descer</Button>
          <span className="flex-1" />
          {confirming ? (
            <Button type="button" size="sm" variant="destructive" autoFocus disabled={remove.isPending} onClick={() => remove.mutate()}>{remove.isPending ? <Spinner aria-hidden /> : <Trash2 aria-hidden />}Confirmar exclusão</Button>
          ) : (
            <Button type="button" size="sm" variant="ghost" className="text-destructive hover:bg-destructive/15" disabled={!editable} onClick={() => setConfirming(true)}><Trash2 aria-hidden />Excluir cargo</Button>
          )}
        </div>
      )}

      {error ? <FieldError>{errorMessage(error)}</FieldError> : null}

      <div>
        <span className="section-label">Permissões</span>
        <ul className="mt-2 space-y-1">
          {permissionList().map(([bit, label]) => (
            <li key={bit} className="flex items-center gap-3 rounded-md bg-surface-raised px-3 py-2">
              <label htmlFor={`perm-${bit}`} className="min-w-0 flex-1 text-sm text-fog">{label}</label>
              <Switch
                id={`perm-${bit}`}
                checked={has(role.permissions, bit)}
                // Nobody grants or revokes what they do not hold.
                disabled={!editable || save.isPending || !has(guild.permissions, bit)}
                onCheckedChange={(on) => save.mutate({ permissions: on ? role.permissions | bit : role.permissions & ~bit })}
              />
            </li>
          ))}
        </ul>
      </div>
    </div>
  )
}

const Roles = ({ guild, myTop }: { guild: Guild; myTop: number }) => {
  const sorted = guild.roles // the server sends them highest first
  const [selectedId, setSelectedId] = useState(guild.id)
  const create = useMutation({ mutationFn: () => api.createRole(guild.id, {}), onSuccess: (r) => setSelectedId(r.id) })
  const role = sorted.find((r) => r.id === selectedId) ?? sorted.find((r) => r.id === guild.id)
  const movable = sorted.filter((r) => r.id !== guild.id)
  const i = role ? movable.indexOf(role) : -1

  return (
    <div className="grid gap-4 sm:grid-cols-[180px_1fr]">
      <div className="space-y-2">
        <Button type="button" size="sm" className="w-full" onClick={() => create.mutate()} disabled={create.isPending}>{create.isPending ? <Spinner aria-hidden /> : <Plus aria-hidden />}Criar cargo</Button>
        {create.error ? <FieldError>{errorMessage(create.error)}</FieldError> : null}
        <ul className="space-y-0.5">
          {sorted.map((r) => (
            <li key={r.id}>
              <button type="button" onClick={() => setSelectedId(r.id)} className={cn('flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm text-greyple hover:bg-surface-hover hover:text-snow', r.id === role?.id && 'bg-surface-active text-snow')}>
                <span aria-hidden className="size-2.5 shrink-0 rounded-full bg-greyple" style={{ backgroundColor: roleColor(r) }} />
                <span className="min-w-0 flex-1 truncate">{r.id === guild.id ? '@everyone' : r.name}</span>
              </button>
            </li>
          ))}
        </ul>
      </div>
      {role ? (
        <RoleEditor
          key={role.id}
          guild={guild}
          role={role}
          editable={role.position < myTop}
          above={i > 0 && movable[i - 1]!.position < myTop ? movable[i - 1] : undefined}
          below={i >= 0 ? movable[i + 1] : undefined}
          onDeleted={() => setSelectedId(guild.id)}
        />
      ) : null}
    </div>
  )
}

// ---- members ----

const MemberRow = ({ guild, member, myTop, meId }: { guild: Guild; member: Member; myTop: number; meId: string }) => {
  const [confirming, setConfirming] = useState<'kick' | 'ban' | null>(null)
  const name = displayName(member.user, member.nickname)
  const isOwner = member.user.id === guild.owner_id
  const actionable = !isOwner && member.user.id !== meId && topPosition(guild, member.user.id) < myTop
  const canRoles = actionable && has(guild.permissions, Permission.ManageRoles)
  const assignable = (r: Role) => canRoles && r.id !== guild.id && r.position < myTop
  const held = guild.roles.filter((r) => member.roles.includes(r.id))
  const addable = guild.roles.filter((r) => assignable(r) && !member.roles.includes(r.id))

  const setRole = useMutation({ mutationFn: ({ roleId, on }: { roleId: string; on: boolean }) => api.setMemberRole(guild.id, member.user.id, roleId, on) })
  const punish = useMutation({
    mutationFn: (what: 'kick' | 'ban') => (what === 'kick' ? api.kick : api.ban)(guild.id, member.user.id),
    onSettled: () => setConfirming(null),
  })
  const error = setRole.error ?? punish.error

  return (
    <li className="space-y-2 rounded-lg border border-white/10 bg-surface-raised p-3">
      <div className="flex items-center gap-3">
        <Avatar userId={member.user.id} displayName={name} avatarUrl={member.user.avatar_url} size="small" ringClass="ring-surface-raised" />
        <div className="min-w-0 flex-1">
          <p className="flex items-center gap-1.5 truncate text-sm font-medium text-snow">
            {name}
            {isOwner ? <Crown aria-label="Dono do servidor" className="size-3.5 shrink-0 text-amber-400" /> : null}
          </p>
          <p className="truncate text-xs text-greyple">@{member.user.username}</p>
        </div>
        {actionable && confirming ? (
          <>
            <Button size="xs" variant="ghost" onClick={() => setConfirming(null)}>Cancelar</Button>
            <Button size="xs" variant="destructive" autoFocus disabled={punish.isPending} onClick={() => punish.mutate(confirming)}>
              {punish.isPending ? <Spinner aria-hidden /> : null}{confirming === 'kick' ? 'Expulsar' : 'Banir'} {name}
            </Button>
          </>
        ) : actionable ? (
          <>
            {has(guild.permissions, Permission.KickMembers) ? <Button size="icon-sm" variant="ghost" className="text-destructive hover:bg-destructive/15" aria-label={`Expulsar ${name}`} title="Expulsar" onClick={() => setConfirming('kick')}><UserMinus aria-hidden /></Button> : null}
            {has(guild.permissions, Permission.BanMembers) ? <Button size="icon-sm" variant="ghost" className="text-destructive hover:bg-destructive/15" aria-label={`Banir ${name}`} title="Banir" onClick={() => setConfirming('ban')}><Ban aria-hidden /></Button> : null}
          </>
        ) : null}
      </div>
      {held.length || addable.length ? (
        <div className="flex flex-wrap items-center gap-1">
          {held.map((r) => (
            <span key={r.id} className="inline-flex items-center gap-1 rounded-md border border-white/10 px-1.5 py-0.5 text-xs text-fog">
              <span aria-hidden className="size-2 rounded-full bg-greyple" style={{ backgroundColor: roleColor(r) }} />
              {r.name}
              {assignable(r) ? (
                <button type="button" aria-label={`Tirar o cargo ${r.name}`} className="text-greyple hover:text-snow" disabled={setRole.isPending} onClick={() => setRole.mutate({ roleId: r.id, on: false })}><X className="size-3" aria-hidden /></button>
              ) : null}
            </span>
          ))}
          {addable.length ? (
            <Select value="" onValueChange={(roleId) => setRole.mutate({ roleId, on: true })} disabled={setRole.isPending}>
              <SelectTrigger size="sm" className="h-6 w-auto gap-1 px-1.5 text-xs" aria-label={`Dar cargo a ${name}`}><Plus className="size-3" aria-hidden /><SelectValue placeholder="Cargo" /></SelectTrigger>
              <SelectContent position="popper">{addable.map((r) => <SelectItem key={r.id} value={r.id}>{r.name}</SelectItem>)}</SelectContent>
            </Select>
          ) : null}
        </div>
      ) : null}
      {error ? <FieldError>{errorMessage(error)}</FieldError> : null}
    </li>
  )
}

const Members = ({ guild, myTop, meId }: { guild: Guild; myTop: number; meId: string }) => {
  const [search, setSearch] = useState('')
  const query = search.trim().toLowerCase()
  const filtered = guild.members.filter((m) =>
    !query || displayName(m.user, m.nickname).toLowerCase().includes(query) || m.user.username.toLowerCase().includes(query))

  return (
    <div className="space-y-3">
      <div className="relative">
        <Search className="absolute left-3 top-2.5 size-4 text-greyple" aria-hidden />
        <Input placeholder="Procurar membro..." aria-label="Procurar membro" value={search} onChange={(e) => setSearch(e.target.value)} className="pl-9" />
      </div>
      <p className="text-xs text-greyple">{guild.members.length} membro{guild.members.length === 1 ? '' : 's'}</p>
      <ul className="max-h-[50vh] space-y-1 overflow-y-auto pr-1">
        {filtered.length === 0 ? <li className="py-8 text-center text-sm text-greyple">Nenhum resultado</li>
          : filtered.map((m) => <MemberRow key={m.user.id} guild={guild} member={m} myTop={myTop} meId={meId} />)}
      </ul>
    </div>
  )
}

// ---- invites and bans (not in the store: fetched while the tab is open) ----

const DATE_TIME = new Intl.DateTimeFormat('pt-BR', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' })

const ListState = ({ query, empty }: { query: { isPending: boolean; error: Error | null; data: unknown[] | undefined }; empty: string }) =>
  query.isPending ? <div className="grid place-items-center py-8"><Spinner aria-label="Carregando" /></div>
    : query.error ? <FieldError>{errorMessage(query.error)}</FieldError>
    : query.data?.length === 0 ? <p className="py-8 text-center text-sm text-greyple">{empty}</p>
    : null

const InviteRow = ({ invite }: { invite: Invite }) => {
  const queryClient = useQueryClient()
  const [copied, setCopied] = useState(false)
  const revoke = useMutation({
    mutationFn: () => api.revokeInvite(invite.code),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['invites', invite.guild_id] }),
  })
  const copy = async () => {
    await navigator.clipboard.writeText(inviteLink(invite.code)).catch(() => {})
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }
  const creator = invite.creator ? displayName(invite.creator) : 'Desconhecido'

  return (
    <li className="space-y-1 rounded-lg border border-white/10 bg-surface-raised p-3">
      <div className="flex items-center gap-3">
        {invite.creator ? <Avatar userId={invite.creator.id} displayName={creator} avatarUrl={invite.creator.avatar_url} size="small" ringClass="ring-surface-raised" /> : null}
        <div className="min-w-0 flex-1">
          <p className="truncate font-mono text-sm text-blurple-soft">{invite.code}</p>
          <p className="truncate text-xs text-greyple">
            {creator} · {invite.uses}/{invite.max_uses || '∞'} usos · {invite.expires_at ? `expira ${DATE_TIME.format(new Date(invite.expires_at))}` : 'não expira'}
          </p>
        </div>
        <Button size="icon-sm" variant="ghost" aria-label={`Copiar link do convite ${invite.code}`} title={copied ? 'Copiado' : 'Copiar link'} onClick={() => void copy()}>{copied ? <Check aria-hidden /> : <Copy aria-hidden />}</Button>
        <Button size="icon-sm" variant="ghost" className="text-destructive hover:bg-destructive/15" aria-label={`Revogar convite ${invite.code}`} title="Revogar" disabled={revoke.isPending} onClick={() => revoke.mutate()}>{revoke.isPending ? <Spinner aria-hidden /> : <Trash2 aria-hidden />}</Button>
      </div>
      {revoke.error ? <FieldError>{errorMessage(revoke.error)}</FieldError> : null}
    </li>
  )
}

const Invites = ({ guild }: { guild: Guild }) => {
  const query = useQuery({ queryKey: ['invites', guild.id], queryFn: () => api.invites(guild.id) })
  return (
    <div className="space-y-3">
      <ListState query={query} empty="Nenhum convite ativo" />
      {query.data?.length ? <ul className="max-h-[50vh] space-y-1 overflow-y-auto pr-1">{query.data.map((i) => <InviteRow key={i.code} invite={i} />)}</ul> : null}
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
        <Avatar userId={user.id} displayName={name} avatarUrl={user.avatar_url} size="small" ringClass="ring-surface-raised" />
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium text-snow">{name}</p>
          <p className="truncate text-xs text-greyple">@{user.username}</p>
        </div>
        <Button size="xs" variant="secondary" disabled={unban.isPending} onClick={() => unban.mutate()}>{unban.isPending ? <Spinner aria-hidden /> : null}Desbanir</Button>
      </div>
      {unban.error ? <FieldError>{errorMessage(unban.error)}</FieldError> : null}
    </li>
  )
}

const Bans = ({ guild }: { guild: Guild }) => {
  const query = useQuery({ queryKey: ['bans', guild.id], queryFn: () => api.bans(guild.id) })
  return (
    <div className="space-y-3">
      <ListState query={query} empty="Ninguém está banido" />
      {query.data?.length ? <ul className="max-h-[50vh] space-y-1 overflow-y-auto pr-1">{query.data.map((b) => <BanRow key={b.user.id} guildId={guild.id} user={b.user} />)}</ul> : null}
    </div>
  )
}

// ---- delete ----

const DeleteGuild = ({ guild }: { guild: Guild }) => {
  const [typed, setTyped] = useState('')
  // GUILD_DELETE then closes everything and the workspace goes home.
  const remove = useMutation({ mutationFn: () => api.deleteGuild(guild.id) })
  const submit = (e: FormEvent) => {
    e.preventDefault()
    remove.mutate()
  }
  return (
    <form onSubmit={submit} className="space-y-4">
      <p className="text-sm text-fog">Isso apaga o servidor, todos os canais e mensagens. Não dá para desfazer.</p>
      <div>
        <label htmlFor="delete-guild-name" className="section-label">Digite <strong className="text-snow">{guild.name}</strong> para confirmar</label>
        <Input id="delete-guild-name" className="mt-2" value={typed} onChange={(e) => setTyped(e.target.value)} autoComplete="off" />
      </div>
      {remove.error ? <FieldError>{errorMessage(remove.error)}</FieldError> : null}
      <Button type="submit" variant="destructive" disabled={typed !== guild.name || remove.isPending}>{remove.isPending ? <Spinner aria-hidden /> : <Trash2 aria-hidden />}Excluir servidor</Button>
    </form>
  )
}

export const ServerSettingsDialog = ({ guild, onClose }: { guild: Guild; onClose: () => void }) => {
  const me = useMe()
  if (!me) return null
  const myTop = topPosition(guild, me.id)
  const can = (bits: number) => has(guild.permissions, bits)
  const sections = [
    { value: 'overview', label: 'Visão geral', show: can(Permission.ManageGuild) },
    { value: 'roles', label: 'Cargos', show: can(Permission.ManageRoles) },
    { value: 'members', label: 'Membros', show: can(Permission.ManageRoles) || can(Permission.KickMembers) || can(Permission.BanMembers) },
    { value: 'invites', label: 'Convites', show: can(Permission.ManageGuild) },
    { value: 'bans', label: 'Banimentos', show: can(Permission.BanMembers) },
    { value: 'delete', label: 'Excluir servidor', show: guild.owner_id === me.id },
  ].filter((s) => s.show)

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[90vh] overflow-y-auto bg-surface-content sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>Configurações do servidor</DialogTitle>
          <DialogDescription>{guild.name}</DialogDescription>
        </DialogHeader>
        {sections.length === 0 ? <p className="text-sm text-greyple">Você não tem permissão para mudar nada aqui.</p> : (
          <Tabs defaultValue={sections[0]!.value}>
            <TabsList className="max-w-full overflow-x-auto">
              {sections.map((s) => <TabsTrigger key={s.value} value={s.value} className={cn(s.value === 'delete' && 'text-destructive')}>{s.label}</TabsTrigger>)}
            </TabsList>
            <TabsContent value="overview" className="pt-2"><Overview guild={guild} /></TabsContent>
            <TabsContent value="roles" className="pt-2"><Roles guild={guild} myTop={myTop} /></TabsContent>
            <TabsContent value="members" className="pt-2"><Members guild={guild} myTop={myTop} meId={me.id} /></TabsContent>
            <TabsContent value="invites" className="pt-2"><Invites guild={guild} /></TabsContent>
            <TabsContent value="bans" className="pt-2"><Bans guild={guild} /></TabsContent>
            <TabsContent value="delete" className="pt-2"><DeleteGuild guild={guild} /></TabsContent>
          </Tabs>
        )}
      </DialogContent>
    </Dialog>
  )
}
