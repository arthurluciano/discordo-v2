// A channel's settings: rename, topic, delete, and its permission overwrites.
import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Check, Slash, Trash2, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { FieldError } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectLabel, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { api } from '@/lib/api'
import { CHANNEL_SCOPED, has, Permission } from '@/lib/permissions'
import { cn } from '@/lib/utils'
import { ChannelType, type Channel, type Guild } from '@/lib/wire'
import { displayName, useMe } from '../identity/users'
import { bitState, permissionList, setBitState, type TriState } from './logic'

type Target = { id: string; isMember: boolean }

const targetName = (guild: Guild, t: Target) => {
  if (t.isMember) {
    const m = guild.members.find((x) => x.user.id === t.id)
    return m ? displayName(m.user, m.nickname) : 'Membro'
  }
  return t.id === guild.id ? '@everyone' : guild.roles.find((r) => r.id === t.id)?.name ?? 'Cargo'
}

const STATES: { state: TriState; label: string; icon: typeof Check; on: string }[] = [
  { state: 'deny', label: 'Negar', icon: X, on: 'bg-ekko-red text-white' },
  { state: 'inherit', label: 'Herdar', icon: Slash, on: 'bg-surface-active text-snow' },
  { state: 'allow', label: 'Permitir', icon: Check, on: 'bg-spring-green text-black' },
]

const Overview = ({ channel, onDeleted }: { channel: Channel; onDeleted: () => void }) => {
  const [name, setName] = useState(channel.name)
  const [topic, setTopic] = useState(channel.topic)
  const [confirming, setConfirming] = useState(false)
  const save = useMutation({ mutationFn: () => api.updateChannel(channel.id, { name: name.trim(), topic: topic.trim() }) })
  const remove = useMutation({ mutationFn: () => api.deleteChannel(channel.id), onSuccess: onDeleted })
  const submit = (e: FormEvent) => {
    e.preventDefault()
    save.mutate()
  }

  return (
    <form onSubmit={submit} className="space-y-4">
      <div>
        <label htmlFor="channel-settings-name" className="section-label">Nome do canal</label>
        <Input id="channel-settings-name" className="mt-2" maxLength={100} value={name} onChange={(e) => setName(e.target.value)} />
      </div>
      {channel.type === ChannelType.Text ? (
        <div>
          <label htmlFor="channel-settings-topic" className="section-label">Tópico</label>
          <Textarea id="channel-settings-topic" className="mt-2" maxLength={1024} placeholder="Do que se fala aqui?" value={topic} onChange={(e) => setTopic(e.target.value)} />
        </div>
      ) : null}
      <FieldError error={save.error} />
      <FieldError error={remove.error} />
      <div className="flex flex-wrap items-center gap-2">
        <Button type="submit" disabled={!name.trim() || (name === channel.name && topic === channel.topic)} loading={save.isPending}>
          Salvar
        </Button>
        <span className="flex-1" />
        {confirming ? (
          <>
            <Button type="button" variant="ghost" onClick={() => setConfirming(false)}>Cancelar</Button>
            <Button type="button" variant="destructive" onClick={() => remove.mutate()} disabled={remove.isPending} autoFocus>
              {remove.isPending ? <Spinner aria-hidden /> : <Trash2 aria-hidden />}Excluir de vez
            </Button>
          </>
        ) : (
          <Button type="button" variant="ghost" className="text-destructive hover:bg-destructive/15" onClick={() => setConfirming(true)}><Trash2 aria-hidden />Excluir canal</Button>
        )}
      </div>
    </form>
  )
}

const Overwrites = ({ guild, channel, mine }: { guild: Guild; channel: Channel; mine: number }) => {
  const [added, setAdded] = useState<Target[]>([])
  const [selected, setSelected] = useState<Target>({ id: guild.id, isMember: false })
  const targets = useMemo(() => {
    const list: Target[] = [{ id: guild.id, isMember: false }]
    for (const t of [...channel.overwrites.map((o) => ({ id: o.target_id, isMember: o.is_member })), ...added]) {
      if (!list.some((x) => x.id === t.id)) list.push(t)
    }
    return list
  }, [guild.id, channel.overwrites, added])
  const overwrite = channel.overwrites.find((o) => o.target_id === selected.id)
  const roles = guild.roles.filter((r) => r.id !== guild.id && !targets.some((t) => t.id === r.id))
  const members = guild.members.filter((m) => !targets.some((t) => t.id === m.user.id))

  const change = useMutation({
    mutationFn: ({ bit, state }: { bit: number; state: TriState }) => {
      const next = setBitState(overwrite, bit, state)
      return next.allow || next.deny
        ? api.setOverwrite(channel.id, selected.id, { is_member: selected.isMember, ...next })
        : api.deleteOverwrite(channel.id, selected.id)
    },
  })
  const clear = useMutation({ mutationFn: () => api.deleteOverwrite(channel.id, selected.id) })

  const add = (value: string) => {
    const t = { id: value.slice(2), isMember: value.startsWith('m:') }
    setAdded((a) => [...a, t])
    setSelected(t)
  }

  return (
    <div className="grid gap-4 sm:grid-cols-[180px_1fr]">
      <div className="space-y-2">
        <ul className="space-y-0.5">
          {targets.map((t) => (
            <li key={t.id}>
              <button type="button" onClick={() => setSelected(t)} className={cn('w-full truncate rounded-md px-2 py-1.5 text-left text-sm text-greyple hover:bg-surface-hover hover:text-snow', t.id === selected.id && 'bg-surface-active text-snow')}>
                {targetName(guild, t)}
              </button>
            </li>
          ))}
        </ul>
        {roles.length + members.length > 0 ? (
          <Select value="" onValueChange={add}>
            <SelectTrigger size="sm" className="w-full" aria-label="Adicionar cargo ou membro"><SelectValue placeholder="Adicionar…" /></SelectTrigger>
            <SelectContent position="popper">
              {roles.length ? <SelectGroup><SelectLabel>Cargos</SelectLabel>{roles.map((r) => <SelectItem key={r.id} value={`r:${r.id}`}>{r.name}</SelectItem>)}</SelectGroup> : null}
              {members.length ? <SelectGroup><SelectLabel>Membros</SelectLabel>{members.map((m) => <SelectItem key={m.user.id} value={`m:${m.user.id}`}>{displayName(m.user, m.nickname)}</SelectItem>)}</SelectGroup> : null}
            </SelectContent>
          </Select>
        ) : null}
      </div>

      <div className="min-w-0 space-y-2">
        <div className="flex items-center gap-2">
          <h3 className="min-w-0 flex-1 truncate text-sm font-semibold text-snow">{targetName(guild, selected)}</h3>
          {overwrite ? <Button type="button" size="xs" variant="ghost" onClick={() => clear.mutate()} disabled={clear.isPending}>Limpar</Button> : null}
        </div>
        <FieldError error={change.error ?? clear.error} />
        <ul className="max-h-80 space-y-1 overflow-y-auto pr-1">
          {permissionList(CHANNEL_SCOPED).map(([bit, label]) => {
            const current = bitState(overwrite, bit)
            return (
              <li key={bit} className="flex items-center gap-2 rounded-md bg-surface-raised px-3 py-1.5">
                <span className="min-w-0 flex-1 truncate text-sm text-fog">{label}</span>
                <div role="radiogroup" aria-label={label} className="flex gap-0.5">
                  {STATES.map(({ state, label: stateLabel, icon: Icon, on }) => (
                    <button
                      key={state}
                      type="button"
                      role="radio"
                      aria-checked={current === state}
                      aria-label={stateLabel}
                      title={stateLabel}
                      disabled={change.isPending || !has(mine, bit)}
                      onClick={() => current !== state && change.mutate({ bit, state })}
                      className={cn('grid size-7 place-items-center rounded-md text-greyple transition-colors hover:bg-surface-hover disabled:opacity-40', current === state && on)}
                    >
                      <Icon className="size-3.5" aria-hidden />
                    </button>
                  ))}
                </div>
              </li>
            )
          })}
        </ul>
      </div>
    </div>
  )
}

export const ChannelSettingsDialog = ({ guild, channelId, onClose }: { guild: Guild; channelId: string; onClose: () => void }) => {
  const me = useMe()
  const channel = guild.channels.find((c) => c.id === channelId)
  useEffect(() => {
    if (!channel) onClose() // deleted, or we lost sight of it
  }, [channel, onClose])
  if (!channel || !me) return null
  const mine = channel.permissions

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="bg-surface-content sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Configurações de {channel.name}</DialogTitle>
          <DialogDescription>Nome, tópico e quem pode fazer o quê neste canal.</DialogDescription>
        </DialogHeader>
        <Tabs defaultValue="overview">
          <TabsList>
            <TabsTrigger value="overview">Visão geral</TabsTrigger>
            {has(mine, Permission.ManageRoles) ? <TabsTrigger value="permissions">Permissões</TabsTrigger> : null}
          </TabsList>
          <TabsContent value="overview" className="pt-2"><Overview channel={channel} onDeleted={onClose} /></TabsContent>
          <TabsContent value="permissions" className="pt-2"><Overwrites guild={guild} channel={channel} mine={mine} /></TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  )
}
