// A guild's header (menu, settings, invites) and channel list, with who is in each voice channel.
import { useState } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import { ChevronDown, Hash, HeadphoneOff, LogOut, MicOff, Plus, Settings, UserPlus, Volume2 } from 'lucide-react'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Avatar } from '@/design/avatar'
import { QUICK_EASE, SPRING_SOFT, useTransition } from '@/design/motion'
import { api } from '@/lib/api'
import { has, Permission } from '@/lib/permissions'
import { useCallers, useStore } from '@/lib/store'
import { cn } from '@/lib/utils'
import { ChannelType, type Guild, type GuildChannel } from '@/lib/wire'
import { displayName, useMe } from '../identity/users'
import { useProfileViewer } from '../identity/profile-viewer'
import { navigate } from '../shell/navigation'
import { useVoice, voice } from '../voice/voice-session'
import { ChannelSettingsDialog } from './channel-settings-dialog'
import { CreateChannelDialog, InviteDialog } from './guild-dialogs'
import { ServerSettingsDialog, settingsSections } from './server-settings-dialog'

type GuildSidebarProps = { guildId: string; channelId: string | null }

type Open = 'invite' | 'settings' | 'create' | 'leave' | null

const GuildHeader = ({ guild, onOpen }: { guild: Guild; onOpen: (what: Open) => void }) => {
  const me = useMe()
  const [menuOpen, setMenuOpen] = useState(false)
  const transition = useTransition(QUICK_EASE)
  const canSettings = me ? settingsSections(guild, me.id).length > 0 : false

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
  const { openProfile } = useProfileViewer()
  const transition = useTransition(SPRING_SOFT)
  const occupants = useCallers(channelId)
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
  const callId = useVoice((s) => s.channelId)
  const canManage = has(guild.permissions, Permission.ManageChannels)

  const select = (c: GuildChannel) => {
    navigate.channel(guild.id, c.id)
    if (c.type === ChannelType.Voice && callId !== c.id && has(c.permissions, Permission.Connect)) voice.join(c.id)
  }

  return (
    <nav aria-label="Canais" className="min-h-0 flex-1 space-y-4 overflow-y-auto px-2 py-3">
      {SECTIONS.map(({ type, label, icon: Icon }) => {
        const channels = guild.channels.filter((c) => c.type === type)
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
                        <button type="button" aria-current={active ? 'page' : undefined} onClick={() => select(c)} className={cn('flex min-w-0 flex-1 items-center gap-2 px-2 py-1.5 text-sm text-greyple hover:text-snow', active && 'text-snow', callId === c.id && 'text-spring-green')}>
                          <Icon aria-hidden className="size-[18px] shrink-0" />
                          <span className="min-w-0 flex-1 truncate text-left">{c.name}</span>
                        </button>
                        {has(c.permissions, Permission.ManageChannels) ? (
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
      {/* The GUILD_DELETE that follows makes the workspace go home. */}
      <ConfirmDialog {...dialog('leave')} title={`Sair de ${guild.name}?`} description="Você só volta com um novo convite."
        confirmLabel="Sair do servidor" onConfirm={() => api.leaveGuild(guild.id)} />
      {open === 'settings' ? <ServerSettingsDialog guild={guild} onClose={() => setOpen(null)} /> : null}
      {settingsChannelId ? <ChannelSettingsDialog guild={guild} channelId={settingsChannelId} onClose={() => setSettingsChannelId(null)} /> : null}
    </>
  )
}
