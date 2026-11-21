import { useMemo, useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { ArrowLeft, Ban, Check, MessageCircle, UserMinus, UserRoundPlus, X } from 'lucide-react'
import { AnimatePresence, motion } from 'motion/react'
import { Button } from '@/components/ui/button'
import { FieldError } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Spinner } from '@/components/ui/spinner'
import { Avatar } from '@/design/avatar'
import { useProfileViewer } from '@/features/identity/profile-viewer'
import { displayName } from '@/features/identity/users'
import { api, errorMessage } from '@/lib/api'
import { filled } from '@/lib/forms'
import { useStore } from '@/lib/store'
import { cn } from '@/lib/utils'
import { RelationshipStatus, type Relationship } from '@/lib/wire'
import { navigate } from '../shell/navigation'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { friendsIn, type FriendTab } from './dm'

const TABS: readonly { id: FriendTab | 'add'; label: string }[] = [
  { id: 'online', label: 'Online' },
  { id: 'all', label: 'Todos' },
  { id: 'pending', label: 'Pendentes' },
  { id: 'blocked', label: 'Bloqueados' },
  { id: 'add', label: 'Adicionar amigo' },
]

const STATUS_TEXT: Record<number, string> = {
  [RelationshipStatus.Incoming]: 'Pedido de amizade recebido',
  [RelationshipStatus.Outgoing]: 'Pedido de amizade enviado',
  [RelationshipStatus.Blocked]: 'Bloqueado',
}

const EMPTY: Record<FriendTab, string> = {
  online: 'Nenhum amigo online agora.',
  all: 'Você ainda não tem amigos. Adicione alguém pelo nome de usuário.',
  pending: 'Nenhum pedido de amizade pendente.',
  blocked: 'Você não bloqueou ninguém.',
}

const FriendRow = ({ rel }: { rel: Relationship }) => {
  const { user, status } = rel
  const name = displayName(user)
  const { openProfile } = useProfileViewer()
  const [confirming, setConfirming] = useState(false)
  const act = useMutation({ mutationFn: (fn: () => Promise<unknown>) => fn() })
  const icon = (label: string, Icon: typeof Check, fn: () => Promise<unknown>, className?: string) => (
    <Button
      variant="secondary"
      size="icon-sm"
      disabled={act.isPending}
      onClick={() => act.mutate(fn)}
      aria-label={label}
      title={label}
    >
      <Icon className={className} aria-hidden />
    </Button>
  )

  return (
    <motion.article
      layout
      initial={{ opacity: 0, y: 5 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, x: -8 }}
      className="group flex items-center gap-3 py-3"
    >
      <button
        type="button"
        onClick={() => openProfile(user.id)}
        className="flex min-w-0 flex-1 items-center gap-3 text-left"
        aria-label={`Abrir perfil de ${name}`}
      >
        <Avatar
          userId={user.id}
          displayName={name}
          avatarUrl={user.avatar_url}
          state={status === RelationshipStatus.Friend ? (rel.online ? 'online' : 'offline') : undefined}
          ringClass="ring-surface-content"
        />
        <span className="min-w-0 flex-1">
          <span className="flex items-baseline gap-1.5">
            <span className="truncate text-sm font-semibold text-snow">{name}</span>
            <span className="truncate text-xs text-greyple">@{user.username}</span>
          </span>
          <span className="block truncate text-xs text-greyple">
            {STATUS_TEXT[status] ?? (rel.online ? 'Online' : 'Offline')}
          </span>
        </span>
      </button>
      <div className="flex items-center gap-1">
        {status === RelationshipStatus.Friend ? (
          <>
            {icon(`Enviar mensagem para ${name}`, MessageCircle, async () =>
              navigate.dm((await api.openDM(user.id)).id),
            )}
            <Button
              variant="secondary"
              size="icon-sm"
              onClick={() => setConfirming(true)}
              aria-label={`Desfazer amizade com ${name}`}
              title="Desfazer amizade"
            >
              <UserMinus aria-hidden />
            </Button>
            {icon(`Bloquear ${name}`, Ban, () => api.block(user.id))}
          </>
        ) : status === RelationshipStatus.Incoming ? (
          <>
            {icon(`Aceitar pedido de ${name}`, Check, () => api.acceptFriend(user.id), 'text-spring-green')}
            {icon(`Recusar pedido de ${name}`, X, () => api.removeRelationship(user.id))}
            {icon(`Bloquear ${name}`, Ban, () => api.block(user.id))}
          </>
        ) : status === RelationshipStatus.Outgoing ? (
          <Button
            variant="secondary"
            size="sm"
            disabled={act.isPending}
            onClick={() => act.mutate(() => api.removeRelationship(user.id))}
          >
            Cancelar
          </Button>
        ) : (
          <Button
            variant="secondary"
            size="sm"
            disabled={act.isPending}
            onClick={() => act.mutate(() => api.removeRelationship(user.id))}
          >
            Desbloquear
          </Button>
        )}
      </div>
      {act.error ? <FieldError className="max-w-40">{errorMessage(act.error)}</FieldError> : null}
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={`Desfazer amizade com ${name}?`}
        description="Vocês deixam de ser amigos. Para voltar, um dos dois precisa enviar outro pedido."
        confirmLabel="Desfazer amizade"
        onConfirm={() => api.removeRelationship(user.id)}
      />
    </motion.article>
  )
}

const AddFriend = () => {
  const [sent, setSent] = useState('')
  const { register, handleSubmit, reset, formState } = useForm({ defaultValues: { username: '' }, mode: 'onChange' })

  const add = useMutation({
    mutationFn: (name: string) => api.addFriend(name),
    onSuccess: (_, name) => {
      setSent(name)
      reset()
    },
  })

  const field = register('username', {
    ...filled,
    // A new attempt clears the last outcome.
    onChange: () => {
      setSent('')
      add.reset()
    },
  })

  return (
    <div className="px-4 py-5 sm:px-7">
      <h2 className="text-lg font-bold">Adicionar amigo</h2>
      <p className="mt-1 text-sm text-fog">Envie um pedido de amizade pelo nome de usuário.</p>

      <form
        onSubmit={handleSubmit(({ username }) => add.mutate(username.trim().replace(/^@/, '')))}
        className="mt-4 flex max-w-3xl gap-2"
      >
        <label htmlFor="add-friend" className="sr-only">
          Nome de usuário
        </label>
        <Input id="add-friend" placeholder="nome de usuário" autoComplete="off" {...field} />
        <Button type="submit" disabled={!formState.isValid || add.isPending}>
          {add.isPending ? <Spinner aria-hidden /> : <UserRoundPlus aria-hidden />}
          Enviar pedido
        </Button>
      </form>

      <FieldError className="mt-2" error={add.error} />

      {sent ? (
        <p className="mt-2 text-sm text-spring-green" role="status">
          Pedido enviado para @{sent}.
        </p>
      ) : null}
    </div>
  )
}

export const FriendsView = ({ onBack }: { onBack: () => void }) => {
  const [tab, setTab] = useState<FriendTab | 'add'>('online')
  const relationships = useStore((s) => s.relationships)
  const rows = useMemo(() => (tab === 'add' ? [] : friendsIn(relationships, tab)), [relationships, tab])

  return (
    <section
      className="flex h-full min-h-0 min-w-0 flex-1 flex-col bg-surface-content text-snow"
      aria-labelledby="friends-title"
    >
      <header className="flex min-h-12 shrink-0 flex-wrap items-center gap-2 border-b border-white/5 px-3 py-2 sm:px-4">
        <Button variant="ghost" size="icon-sm" onClick={onBack} className="md:hidden" aria-label="Voltar">
          <ArrowLeft aria-hidden />
        </Button>
        <div className="mr-1 flex items-center gap-2 font-bold">
          <UserRoundPlus className="size-5 text-fog" aria-hidden />
          <h1 id="friends-title">Amigos</h1>
        </div>
        <span className="hidden h-6 w-px bg-white/10 sm:block" aria-hidden />
        <div className="flex min-w-0 flex-1 gap-1 overflow-x-auto" role="tablist" aria-label="Filtros de amigos">
          {TABS.map((item) => (
            <button
              key={item.id}
              type="button"
              role="tab"
              aria-selected={tab === item.id}
              onClick={() => setTab(item.id)}
              className={cn(
                'shrink-0 rounded-md px-2.5 py-1.5 text-xs font-semibold transition-colors sm:text-sm',
                item.id === 'add'
                  ? tab === item.id
                    ? 'bg-spring-green text-white'
                    : 'text-spring-green hover:bg-spring-green/10'
                  : tab === item.id
                    ? 'bg-surface-active text-snow'
                    : 'text-greyple hover:bg-surface-hover hover:text-fog',
              )}
            >
              {item.label}
            </button>
          ))}
        </div>
      </header>

      {tab === 'add' ? (
        <AddFriend />
      ) : (
        <>
          <div className="px-4 pt-5 sm:px-7">
            <p className="section-label">
              {TABS.find((item) => item.id === tab)?.label} — {rows.length}
            </p>
          </div>
          <ScrollArea className="min-h-0 flex-1 px-4 pb-[max(1rem,env(safe-area-inset-bottom))] sm:px-7">
            <div className="mt-2 max-w-3xl divide-y divide-white/5">
              <AnimatePresence mode="popLayout" initial={false}>
                {rows.map((rel) => (
                  <FriendRow key={rel.user.id} rel={rel} />
                ))}
              </AnimatePresence>
              {rows.length === 0 ? (
                <div className="py-16 text-center">
                  <Ban className="mx-auto size-9 text-greyple" aria-hidden />
                  <p className="mt-3 font-semibold text-snow">Nada por aqui</p>
                  <p className="mt-1 text-sm text-fog">{EMPTY[tab]}</p>
                </div>
              ) : null}
            </div>
          </ScrollArea>
        </>
      )}
    </section>
  )
}
