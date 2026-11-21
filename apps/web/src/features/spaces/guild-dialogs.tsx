// The guild header's dialogs: create a channel, invite people.
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Controller, useForm } from 'react-hook-form'
import { Check, Copy, Hash, UserPlus, Volume2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { FieldError } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api } from '@/lib/api'
import { filled } from '@/lib/forms'
import { inviteLink } from '@/lib/pending-invite'
import { useCopy } from '@/lib/use-copy'
import { cn } from '@/lib/utils'
import { ChannelType, type Guild } from '@/lib/wire'
import { INVITE_EXPIRY } from './logic'

// The world (type 4) comes with the server: it cannot be created.
type NewChannel = { name: string; type: typeof ChannelType.Text | typeof ChannelType.Voice }

const CHANNEL_TYPES = [
  [ChannelType.Text, 'Texto', Hash],
  [ChannelType.Voice, 'Voz', Volume2],
] as const

export const CreateChannelDialog = ({
  guildId,
  open,
  onOpenChange,
}: {
  guildId: string
  open: boolean
  onOpenChange: (open: boolean) => void
}) => {
  const { control, register, handleSubmit, reset, formState } = useForm<NewChannel>({
    defaultValues: { name: '', type: ChannelType.Text },
    mode: 'onChange',
  })

  const create = useMutation({
    mutationFn: ({ name, type }: NewChannel) => api.createChannel(guildId, { name: name.trim(), type }),
    onSuccess: () => {
      reset()
      onOpenChange(false)
    },
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="bg-surface-content sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Criar canal</DialogTitle>
          <DialogDescription>Canais de texto para conversar, de voz para chamadas.</DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit((values) => create.mutate(values))} className="space-y-4">
          <Controller
            control={control}
            name="type"
            render={({ field }) => (
              <div className="grid grid-cols-2 gap-2" role="radiogroup" aria-label="Tipo de canal">
                {CHANNEL_TYPES.map(([value, label, Icon]) => (
                  <button
                    key={value}
                    type="button"
                    role="radio"
                    aria-checked={field.value === value}
                    onClick={() => field.onChange(value)}
                    className={cn(
                      'flex items-center gap-2 rounded-lg border border-white/10 bg-surface-raised px-3 py-2.5 text-sm text-greyple transition-colors hover:text-snow',
                      field.value === value && 'border-blurple-soft text-snow',
                    )}
                  >
                    <Icon className="size-4" aria-hidden />
                    {label}
                  </button>
                ))}
              </div>
            )}
          />

          <div>
            <label htmlFor="channel-name" className="section-label">
              Nome do canal
            </label>
            <Input
              id="channel-name"
              className="mt-2"
              placeholder="novo-canal"
              maxLength={100}
              autoFocus
              {...register('name', filled)}
            />
          </div>

          <FieldError error={create.error} />

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancelar
            </Button>
            <Button type="submit" disabled={!formState.isValid} loading={create.isPending}>
              Criar canal
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export const InviteDialog = ({
  guild,
  open,
  onOpenChange,
}: {
  guild: Guild
  open: boolean
  onOpenChange: (open: boolean) => void
}) => {
  const [maxUses, setMaxUses] = useState('0')
  const [maxAge, setMaxAge] = useState(String(INVITE_EXPIRY[3].seconds))
  const queryClient = useQueryClient()
  const create = useMutation({
    mutationFn: () => api.createInvite(guild.id, { max_uses: Number(maxUses), max_age: Number(maxAge) }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['invites', guild.id] }),
  })
  const link = create.data ? inviteLink(create.data.code) : ''
  const { copied, copy } = useCopy()

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) create.reset()
        onOpenChange(o)
      }}
    >
      <DialogContent className="bg-surface-content sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <UserPlus className="size-5 text-blurple-soft" aria-hidden />
            Convidar para {guild.name}
          </DialogTitle>
          <DialogDescription>Gere um link e envie para quem você quer chamar.</DialogDescription>
        </DialogHeader>
        <div className="grid grid-cols-2 gap-3">
          <div>
            <span className="section-label">Expira em</span>
            <Select value={maxAge} onValueChange={setMaxAge}>
              <SelectTrigger className="mt-2 w-full" aria-label="Expira em">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {INVITE_EXPIRY.map((o) => (
                  <SelectItem key={o.seconds} value={String(o.seconds)}>
                    {o.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div>
            <span className="section-label">Máximo de usos</span>
            <Select value={maxUses} onValueChange={setMaxUses}>
              <SelectTrigger className="mt-2 w-full" aria-label="Máximo de usos">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {[0, 1, 5, 10, 25, 50, 100].map((n) => (
                  <SelectItem key={n} value={String(n)}>
                    {n === 0 ? 'Sem limite' : `${n} ${n === 1 ? 'uso' : 'usos'}`}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>
        {link ? (
          <div className="flex items-center gap-2 rounded-lg border border-white/10 bg-surface-raised p-2 pl-3">
            <span className="min-w-0 flex-1 truncate font-mono text-sm text-blurple-soft">{link}</span>
            <Button type="button" size="sm" onClick={() => void copy(link)}>
              {copied ? <Check aria-hidden /> : <Copy aria-hidden />}
              {copied ? 'Copiado' : 'Copiar'}
            </Button>
          </div>
        ) : null}
        <FieldError error={create.error} />
        <Button
          type="button"
          variant={link ? 'secondary' : 'default'}
          onClick={() => create.mutate()}
          loading={create.isPending}
        >
          {link ? 'Gerar outro link' : 'Gerar link'}
        </Button>
      </DialogContent>
    </Dialog>
  )
}
