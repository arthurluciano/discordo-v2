// A guild's header (menu, settings, invites) and channel list, with who is in each voice channel.
import { useMemo, useState, type FormEvent } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import { useMutation } from '@tanstack/react-query'
import { Check, ChevronDown, Copy, Hash, HeadphoneOff, LogOut, MicOff, Plus, Settings, UserPlus, Volume2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { FieldError } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Avatar } from '@/design/avatar'
import { QUICK_EASE, SPRING_SOFT, useTransition } from '@/design/motion'
import { api, errorMessage } from '@/lib/api'
import { channelPermissions, has, Permission } from '@/lib/permissions'
import { inviteLink } from '@/lib/pending-invite'
import { useStore } from '@/lib/store'
import { cn } from '@/lib/utils'
import { ChannelType, type Channel, type Guild } from '@/lib/wire'
import { displayName, useMe } from '../identity/users'
import { useProfileViewer } from '../identity/profile-viewer'
import { navigate } from '../shell/navigation'
import { useVoice } from '../voice/voice-session'
import { ChannelSettingsDialog } from './channel-settings-dialog'
import { INVITE_EXPIRY } from './logic'
import { ServerSettingsDialog } from './server-settings-dialog'

export type GuildSidebarProps = { guildId: string; channelId: string | null }

type Open = 'invite' | 'settings' | 'create' | 'leave' | null

const SETTINGS_PERMISSIONS = [Permission.ManageGuild, Permission.ManageRoles, Permission.ManageChannels, Permission.KickMembers, Permission.BanMembers]

const CreateChannelDialog = ({ guildId, open, onOpenChange }: { guildId: string; open: boolean; onOpenChange: (open: boolean) => void }) => {
  const [name, setName] = useState('')
  const [type, setType] = useState<Channel['type']>(ChannelType.Text)
  const create = useMutation({
    mutationFn: () => api.createChannel(guildId, { name: name.trim(), type }),
    onSuccess: () => {
      setName('')
      onOpenChange(false)
    },
  })
  const submit = (e: FormEvent) => {
    e.preventDefault()
    create.mutate()
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="bg-surface-content sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Criar canal</DialogTitle>
          <DialogDescription>Canais de texto para conversar, de voz para chamadas.</DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-4">
          <div className="grid grid-cols-2 gap-2" role="radiogroup" aria-label="Tipo de canal">
            {([[ChannelType.Text, 'Texto', Hash], [ChannelType.Voice, 'Voz', Volume2]] as const).map(([value, label, Icon]) => (
              <button key={value} type="button" role="radio" aria-checked={type === value} onClick={() => setType(value)} className={cn('flex items-center gap-2 rounded-lg border border-white/10 bg-surface-raised px-3 py-2.5 text-sm text-greyple transition-colors hover:text-snow', type === value && 'border-blurple-soft text-snow')}>
                <Icon className="size-4" aria-hidden />{label}
              </button>
            ))}
          </div>
          <div>
            <label htmlFor="channel-name" className="section-label">Nome do canal</label>
            <Input id="channel-name" className="mt-2" placeholder="novo-canal" maxLength={100} value={name} onChange={(e) => setName(e.target.value)} autoFocus />
          </div>
          {create.error ? <FieldError>{errorMessage(create.error)}</FieldError> : null}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>Cancelar</Button>
            <Button type="submit" disabled={create.isPending || !name.trim()}>{create.isPending ? <Spinner aria-hidden /> : null}Criar canal</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

const InviteDialog = ({ guild, open, onOpenChange }: { guild: Guild; open: boolean; onOpenChange: (open: boolean) => void }) => {
  const [maxUses, setMaxUses] = useState('0')
  const [maxAge, setMaxAge] = useState(String(INVITE_EXPIRY[3].seconds))
  const [copied, setCopied] = useState(false)
  const create = useMutation({ mutationFn: () => api.createInvite(guild.id, { max_uses: Number(maxUses), max_age: Number(maxAge) }) })
  const link = create.data ? inviteLink(create.data.code) : ''
  const copy = async () => {
    await navigator.clipboard.writeText(link).catch(() => {})
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <Dialog open={open} onOpenChange={(o) => { if (!o) create.reset(); onOpenChange(o) }}>
      <DialogContent className="bg-surface-content sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><UserPlus className="size-5 text-blurple-soft" aria-hidden />Convidar para {guild.name}</DialogTitle>
          <DialogDescription>Gere um link e envie para quem você quer chamar.</DialogDescription>
        </DialogHeader>
        <div className="grid grid-cols-2 gap-3">
          <div>
            <span className="section-label">Expira em</span>
            <Select value={maxAge} onValueChange={setMaxAge}>
              <SelectTrigger className="mt-2 w-full" aria-label="Expira em"><SelectValue /></SelectTrigger>
              <SelectContent>{INVITE_EXPIRY.map((o) => <SelectItem key={o.seconds} value={String(o.seconds)}>{o.label}</SelectItem>)}</SelectContent>
            </Select>
          </div>
          <div>
            <span className="section-label">Máximo de usos</span>
            <Select value={maxUses} onValueChange={setMaxUses}>
              <SelectTrigger className="mt-2 w-full" aria-label="Máximo de usos"><SelectValue /></SelectTrigger>
              <SelectContent>{[0, 1, 5, 10, 25, 50, 100].map((n) => <SelectItem key={n} value={String(n)}>{n === 0 ? 'Sem limite' : `${n} ${n === 1 ? 'uso' : 'usos'}`}</SelectItem>)}</SelectContent>
            </Select>
          </div>
        </div>
        {link ? (
          <div className="flex items-center gap-2 rounded-lg border border-white/10 bg-surface-raised p-2 pl-3">
            <span className="min-w-0 flex-1 truncate font-mono text-sm text-blurple-soft">{link}</span>
            <Button type="button" size="sm" onClick={() => void copy()}>{copied ? <Check aria-hidden /> : <Copy aria-hidden />}{copied ? 'Copiado' : 'Copiar'}</Button>
          </div>
        ) : null}
        {create.error ? <FieldError>{errorMessage(create.error)}</FieldError> : null}
        <Button type="button" variant={link ? 'secondary' : 'default'} onClick={() => create.mutate()} disabled={create.isPending}>
          {create.isPending ? <Spinner aria-hidden /> : null}{link ? 'Gerar outro link' : 'Gerar link'}
        </Button>
      </DialogContent>
    </Dialog>
  )
}

const LeaveDialog = ({ guild, open, onOpenChange }: { guild: Guild; open: boolean; onOpenChange: (open: boolean) => void }) => {
  // The GUILD_DELETE that follows makes the workspace go home.
  const leave = useMutation({ mutationFn: () => api.leaveGuild(guild.id), onSuccess: () => onOpenChange(false) })
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="bg-surface-content sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>Sair de {guild.name}?</DialogTitle>
          <DialogDescription>Você só volta com um novo convite.</DialogDescription>
        </DialogHeader>
        {leave.error ? <FieldError>{errorMessage(leave.error)}</FieldError> : null}
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>Cancelar</Button>
          <Button type="button" variant="destructive" onClick={() => leave.mutate()} disabled={leave.isPending}>{leave.isPending ? <Spinner aria-hidden /> : null}Sair do servidor</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

const GuildHeader = ({ guild, onOpen }: { guild: Guild; onOpen: (what: Open) => void }) => {
  const me = useMe()
  const [menuOpen, setMenuOpen] = useState(false)
  const transition = useTransition(QUICK_EASE)
  const canSettings = SETTINGS_PERMISSIONS.some((p) => has(guild.permissions, p))

  return (
    <DropdownMenu open={menuOpen} onOpenChange={setMenuOpen}>
      <DropdownMenuTrigger
        style={guild.banner_url ? { backgroundImage: `linear-gradient(to bottom, transparent, rgb(0 0 0 / 0.6)), url("${guild.banner_url}")` } : undefined}
        className={cn('flex w-full shrink-0 justify-between gap-2 border-b border-white/10 bg-cover bg-center px-4 text-left font-bold text-snow transition-colors focus-visible:outline-none', guild.banner_url ? 'h-28 items-end pb-3 [text-shadow:0_1px_3px_rgb(0_0_0/0.8)]' : 'h-12 items-center hover:bg-surface-hover')}
      >
        <span className="min-w-0 flex-1 truncate">{guild.name}</span>
        <motion.span aria-hidden animate={{ rotate: menuOpen ? 180 : 0 }} transition={transition} className="grid shrink-0 place-items-center"><ChevronDown className="size-4 text-fog" /></motion.span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-56">
        <DropdownMenuItem onSelect={() => onOpen('invite')} disabled={!has(guild.permissions, Permission.CreateInvite)}><UserPlus aria-hidden />Convidar pessoas</DropdownMenuItem>
        {canSettings ? <DropdownMenuItem onSelect={() => onOpen('settings')}><Settings aria-hidden />Configurações do servidor</DropdownMenuItem> : null}
        {has(guild.permissions, Permission.ManageChannels) ? <DropdownMenuItem onSelect={() => onOpen('create')}><Plus aria-hidden />Criar canal</DropdownMenuItem> : null}
        {me && me.id !== guild.owner_id ? (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onSelect={() => onOpen('leave')}><LogOut aria-hidden />Sair do servidor</DropdownMenuItem>
          </>
        ) : null}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

const VoiceOccupants = ({ guild, channelId }: { guild: Guild; channelId: string }) => {
  const voice = useStore((s) => s.voice)
  const { openProfile } = useProfileViewer()
  const transition = useTransition(SPRING_SOFT)
  const occupants = useMemo(() => Object.values(voice).filter((v) => v.channel_id === channelId), [voice, channelId])
  if (occupants.length === 0) return null

  return (
    <ul className="ml-7 mt-0.5 space-y-0.5">
      <AnimatePresence initial={false}>
        {occupants.map((v) => {
          const member = guild.members.find((m) => m.user.id === v.user_id)
          const name = member ? displayName(member.user, member.nickname) : 'Alguém'
          return (
            <motion.li key={v.user_id} layout initial={{ opacity: 0, height: 0 }} animate={{ opacity: 1, height: 'auto' }} exit={{ opacity: 0, height: 0 }} transition={transition} className="overflow-hidden">
              <button type="button" onClick={() => openProfile(v.user_id)} className="flex w-full items-center gap-2 rounded-md px-1.5 py-1 text-left text-xs text-greyple hover:bg-surface-hover hover:text-snow">
                <Avatar userId={v.user_id} displayName={name} avatarUrl={member?.user.avatar_url} size="small" />
                <span className="min-w-0 flex-1 truncate">{name}</span>
                {v.tracks.includes('screen') ? <span className="shrink-0 rounded bg-ekko-red px-1 text-[10px] font-bold leading-4 text-white">AO VIVO</span> : null}
                {v.muted ? <MicOff className="size-3.5 shrink-0 text-ekko-red" aria-label="Microfone mutado" /> : null}
                {v.deafened ? <HeadphoneOff className="size-3.5 shrink-0 text-ekko-red" aria-label="Áudio desativado" /> : null}
              </button>
            </motion.li>
          )
        })}
      </AnimatePresence>
    </ul>
  )
}

const SECTIONS = [
  { type: ChannelType.Text, label: 'Canais de texto', icon: Hash },
  { type: ChannelType.Voice, label: 'Canais de voz', icon: Volume2 },
] as const

const ChannelList = ({ guild, channelId, onCreate, onSettings }: { guild: Guild; channelId: string | null; onCreate: () => void; onSettings: (id: string) => void }) => {
  const me = useMe()
  const voice = useVoice()
  const canManage = has(guild.permissions, Permission.ManageChannels)
  const sorted = useMemo(() => [...guild.channels].sort((a, b) => a.position - b.position || a.id.localeCompare(b.id)), [guild.channels])

  const select = (c: Channel) => {
    navigate.channel(guild.id, c.id)
    if (c.type === ChannelType.Voice && voice.channelId !== c.id && me && has(channelPermissions(guild, c, me.id), Permission.Connect)) voice.join(c.id)
  }

  return (
    <nav aria-label="Canais" className="min-h-0 flex-1 space-y-4 overflow-y-auto px-2 py-3">
      {SECTIONS.map(({ type, label, icon: Icon }) => {
        const channels = sorted.filter((c) => c.type === type)
        if (channels.length === 0 && !canManage) return null
        return (
          <section key={type}>
            <div className="mb-1 flex items-center px-1.5 text-greyple/80">
              <h2 className="flex-1 truncate text-xs font-semibold uppercase">{label}</h2>
              {canManage ? <button type="button" onClick={onCreate} className="hover:text-snow" aria-label="Criar canal"><Plus className="size-3.5" aria-hidden /></button> : null}
            </div>
            {channels.length === 0 ? <p className="px-1.5 py-1 text-xs text-greyple/60">Nenhum canal</p> : (
              <ul className="space-y-0.5">
                {channels.map((c) => {
                  const active = c.id === channelId
                  return (
                    <li key={c.id}>
                      <div className={cn('group flex items-center gap-1 rounded-md pr-1.5 transition-colors hover:bg-surface-hover', active && 'bg-surface-active')}>
                        <button type="button" aria-current={active ? 'page' : undefined} onClick={() => select(c)} className={cn('flex min-w-0 flex-1 items-center gap-2 px-2 py-1.5 text-sm text-greyple hover:text-snow', active && 'text-snow', voice.channelId === c.id && 'text-spring-green')}>
                          <Icon aria-hidden className="size-[18px] shrink-0" />
                          <span className="min-w-0 flex-1 truncate text-left">{c.name}</span>
                        </button>
                        {canManage ? (
                          <button type="button" onClick={() => onSettings(c.id)} className="text-greyple opacity-0 transition-opacity hover:text-snow focus-visible:opacity-100 group-hover:opacity-100 max-md:opacity-100" aria-label={`Configurações de ${c.name}`}>
                            <Settings aria-hidden className="size-3.5" />
                          </button>
                        ) : null}
                      </div>
                      {c.type === ChannelType.Voice ? <VoiceOccupants guild={guild} channelId={c.id} /> : null}
                    </li>
                  )
                })}
              </ul>
            )}
          </section>
        )
      })}
    </nav>
  )
}

export const GuildSidebar = ({ guildId, channelId }: GuildSidebarProps) => {
  const guild = useStore((s) => s.guilds.find((g) => g.id === guildId))
  const [open, setOpen] = useState<Open>(null)
  const [settingsChannelId, setSettingsChannelId] = useState<string | null>(null)
  if (!guild) return null
  const dialog = (what: Exclude<Open, null>) => ({ open: open === what, onOpenChange: (o: boolean) => setOpen(o ? what : null) })

  return (
    <>
      <GuildHeader guild={guild} onOpen={setOpen} />
      <ChannelList guild={guild} channelId={channelId} onCreate={() => setOpen('create')} onSettings={setSettingsChannelId} />
      <InviteDialog guild={guild} {...dialog('invite')} />
      <CreateChannelDialog guildId={guild.id} {...dialog('create')} />
      <LeaveDialog guild={guild} {...dialog('leave')} />
      {open === 'settings' ? <ServerSettingsDialog guild={guild} onClose={() => setOpen(null)} /> : null}
      {settingsChannelId ? <ChannelSettingsDialog guild={guild} channelId={settingsChannelId} onClose={() => setSettingsChannelId(null)} /> : null}
    </>
  )
}
