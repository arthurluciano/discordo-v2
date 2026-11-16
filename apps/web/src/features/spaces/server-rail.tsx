// The column of servers, home, and create/join a server.
import { useState, type FormEvent, type ReactNode } from 'react'
import { motion } from 'motion/react'
import { useMutation } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { FieldError } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { BrandLogo } from '@/design/brand-logo'
import { QUICK_EASE, useTransition } from '@/design/motion'
import { api } from '@/lib/api'
import { setState, useStore } from '@/lib/store'
import { cn } from '@/lib/utils'
import type { Guild } from '@/lib/wire'
import { navigate } from '../shell/navigation'
import { parseInviteCode } from './logic'

type ServerRailProps = { activeGuildId: string | null }

export const initialsOf = (name: string) =>
  name.trim().split(/\s+/).map((w) => w[0]).join('').slice(0, 2).toUpperCase() || '?'

// The REST reply can beat the GUILD_SYNC; without the snapshot in the store the workspace would
// think the guild is gone and send the user home.
const openGuild = (guild: Guild) => {
  setState((s) => (s.guilds.some((g) => g.id === guild.id) ? s : { ...s, guilds: [...s.guilds, guild] }))
  navigate.guild(guild.id)
}

const RailButton = ({ label, active, children, onClick }: { label: string; active: boolean; children: ReactNode; onClick: () => void }) => {
  const transition = useTransition(QUICK_EASE)
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <motion.button
          type="button"
          aria-label={label}
          aria-current={active ? 'page' : undefined}
          whileHover={{ scale: 1.04 }}
          whileTap={{ scale: 0.94 }}
          transition={transition}
          onClick={onClick}
          className={cn('group relative grid size-12 shrink-0 place-items-center rounded-2xl bg-surface-sidebar text-fog transition-[border-radius,background-color,color] duration-150 hover:rounded-xl hover:bg-blurple hover:text-white focus-visible:rounded-xl', active && 'rounded-xl bg-blurple text-white')}
        >
          <span className={cn('absolute -left-3 h-2 w-1 rounded-r-full bg-white transition-all', active ? 'h-10' : 'opacity-0 group-hover:h-5 group-hover:opacity-100')} aria-hidden />
          <span className="grid size-full place-items-center overflow-hidden rounded-[inherit]">{children}</span>
        </motion.button>
      </TooltipTrigger>
      <TooltipContent side="right" sideOffset={8}>{label}</TooltipContent>
    </Tooltip>
  )
}

const AddServerDialog = ({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) => {
  const [name, setName] = useState('')
  const [invite, setInvite] = useState('')
  const done = (guild: Guild) => {
    setName('')
    setInvite('')
    onOpenChange(false)
    openGuild(guild)
  }
  const create = useMutation({ mutationFn: () => api.createGuild(name.trim()), onSuccess: done })
  const join = useMutation({
    mutationFn: () => {
      const code = parseInviteCode(invite)
      if (!code) throw new Error('Convite inválido. Cole o código ou o link inteiro.')
      return api.joinInvite(code)
    },
    onSuccess: done,
  })
  const submit = (mutation: { mutate: () => void }) => (e: FormEvent) => {
    e.preventDefault()
    mutation.mutate()
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="bg-surface-content sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><Plus className="size-5 text-blurple-soft" aria-hidden />Adicionar um servidor</DialogTitle>
          <DialogDescription>Crie o seu próprio espaço ou entre em um com um convite.</DialogDescription>
        </DialogHeader>

        <form onSubmit={submit(create)} className="space-y-2">
          <label htmlFor="new-guild-name" className="section-label">Criar servidor</label>
          <div className="flex gap-2">
            <Input id="new-guild-name" placeholder="Nome do servidor" maxLength={100} value={name} onChange={(e) => setName(e.target.value)} />
            <Button type="submit" disabled={!name.trim()} loading={create.isPending}>Criar</Button>
          </div>
          <FieldError error={create.error} />
        </form>

        <form onSubmit={submit(join)} className="space-y-2 border-t border-white/10 pt-4">
          <label htmlFor="join-invite" className="section-label">Entrar com convite</label>
          <div className="flex gap-2">
            <Input id="join-invite" placeholder="https://…/invite/ABC123 ou ABC123" value={invite} onChange={(e) => setInvite(e.target.value)} />
            <Button type="submit" variant="secondary" disabled={!invite.trim()} loading={join.isPending}>Entrar</Button>
          </div>
          <FieldError error={join.error} />
        </form>
      </DialogContent>
    </Dialog>
  )
}

export const ServerRail = ({ activeGuildId }: ServerRailProps) => {
  const guilds = useStore((s) => s.guilds)
  const [adding, setAdding] = useState(false)

  return (
    <nav aria-label="Servidores" className="dock-clearance flex w-[72px] shrink-0 flex-col items-center gap-2 overflow-y-auto bg-surface-rail py-3 [scrollbar-width:none]">
      <RailButton label="Inicial" active={activeGuildId === null} onClick={navigate.home}>
        <BrandLogo className="size-12 rounded-none" />
      </RailButton>
      {guilds.length > 0 ? <span aria-hidden className="my-0.5 h-0.5 w-8 shrink-0 rounded-full bg-white/10" /> : null}
      {guilds.map((g) => (
        <RailButton key={g.id} label={g.name} active={g.id === activeGuildId} onClick={() => navigate.guild(g.id)}>
          {g.icon_url ? <img src={g.icon_url} alt="" className="size-full object-cover" /> : <span className="text-sm font-bold">{initialsOf(g.name)}</span>}
        </RailButton>
      ))}
      <RailButton label="Adicionar um servidor" active={false} onClick={() => setAdding(true)}>
        <Plus aria-hidden className="size-5 text-spring-green group-hover:text-white" />
      </RailButton>
      <AddServerDialog open={adding} onOpenChange={setAdding} />
    </nav>
  )
}
