import { useRef, useState, type KeyboardEvent } from 'react'
import { Copy, FileText, Pencil, SmilePlus, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Sheet, SheetContent, SheetDescription, SheetTitle } from '@/components/ui/sheet'
import { Textarea } from '@/components/ui/textarea'
import { Avatar } from '@/design/avatar'
import { api, attachmentUrl, errorMessage } from '@/lib/api'
import { cn } from '@/lib/utils'
import type { Attachment, Message } from '@/lib/wire'
import { useProfileViewer } from '../identity/profile-viewer'
import { displayName, useUser } from '../identity/users'
import { formatSize, splitMentions } from './format'

const TIME_FORMAT = new Intl.DateTimeFormat('pt-BR', { hour: '2-digit', minute: '2-digit' })
const DATE_FORMAT = new Intl.DateTimeFormat('pt-BR', { day: '2-digit', month: 'short' })
export const EMOJIS = ['👍', '❤️', '😂', '😮', '😢', '🎉', '🔥', '👀']

/** user ID -> the name to show in this channel (nickname in a guild). */
export type People = Map<string, string>

const MentionChip = ({ userId, people }: { userId: string; people: People }) => {
  const user = useUser(userId)
  const { openProfile } = useProfileViewer()
  const name = people.get(userId) ?? (user ? displayName(user) : null)
  if (!name) return <span className="rounded bg-blurple/20 px-0.5 font-medium text-blurple-soft">@usuário desconhecido</span>
  return <button type="button" onClick={() => openProfile(userId)} className="rounded bg-blurple/20 px-0.5 font-medium text-blurple-soft hover:bg-blurple hover:text-white">@{name}</button>
}

const Content = ({ content, people }: { content: string; people: People }) => (
  <>{splitMentions(content).map((part, i) => (typeof part === 'string' ? part : <MentionChip key={i} userId={part.userId} people={people} />))}</>
)

const AttachmentView = ({ attachment }: { attachment: Attachment }) => {
  const src = attachmentUrl(attachment)
  if (attachment.content_type.startsWith('image/')) {
    return <a href={src} target="_blank" rel="noreferrer" className="block w-fit"><img src={src} alt={attachment.filename} loading="lazy" className="max-h-80 max-w-full rounded-lg object-contain" /></a>
  }
  if (attachment.content_type.startsWith('video/')) return <video src={src} controls preload="metadata" className="max-h-80 max-w-full rounded-lg" />
  return (
    <a href={src} download={attachment.filename} className="flex w-fit max-w-full items-center gap-3 rounded-lg border border-white/10 bg-surface-raised p-3 hover:bg-surface-hover">
      <FileText aria-hidden className="size-8 shrink-0 text-blurple-soft" />
      <span className="min-w-0"><span className="block truncate text-sm font-medium text-cerulean">{attachment.filename}</span><span className="text-xs text-greyple">{formatSize(attachment.size)}</span></span>
    </a>
  )
}

const ReactionPicker = ({ onPick }: { onPick: (emoji: string) => void }) => (
  <DropdownMenu>
    <DropdownMenuTrigger asChild><Button variant="ghost" size="icon-xs" aria-label="Adicionar reação"><SmilePlus aria-hidden /></Button></DropdownMenuTrigger>
    <DropdownMenuContent align="end" className="grid w-auto grid-cols-4 gap-0.5">
      {EMOJIS.map((emoji) => <DropdownMenuItem key={emoji} onSelect={() => onPick(emoji)} className="justify-center text-xl">{emoji}</DropdownMenuItem>)}
    </DropdownMenuContent>
  </DropdownMenu>
)

const Editor = ({ message, onDone }: { message: Message; onDone: () => void }) => {
  const [draft, setDraft] = useState(message.content)
  const [error, setError] = useState('')
  const save = async () => {
    const content = draft.trim()
    if (!content || content === message.content) return onDone()
    try {
      await api.editMessage(message.channel_id, message.id, content)
      onDone()
    } catch (cause) {
      setError(errorMessage(cause))
    }
  }
  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Escape') onDone()
    else if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault()
      void save()
    }
  }
  return (
    <div className="py-1">
      <Textarea autoFocus value={draft} onChange={(e) => setDraft(e.target.value)} onKeyDown={onKeyDown} aria-label="Editar mensagem" className="min-h-10 resize-none bg-surface-raised" />
      <p className="mt-1 text-[0.7rem] text-greyple">Esc para cancelar · Enter para salvar{error ? <span className="ml-2 text-destructive">{error}</span> : null}</p>
    </div>
  )
}

type MessageItemProps = {
  message: Message
  grouped: boolean
  meId: string
  people: People
  /** Author's role color in a guild; undefined in DMs or without a colored role. */
  color: string | undefined
  canReact: boolean
  canManage: boolean
}

export const MessageItem = ({ message, grouped, meId, people, color, canReact, canManage }: MessageItemProps) => {
  const { openProfile } = useProfileViewer()
  const [editing, setEditing] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [error, setError] = useState('')
  const [sheetOpen, setSheetOpen] = useState(false)
  const [pressing, setPressing] = useState(false)
  const pressTimer = useRef<number | undefined>(undefined)
  const own = message.author.id === meId
  const name = people.get(message.author.id) ?? displayName(message.author)
  const at = new Date(message.created_at)
  const highlighted = message.mention_everyone || message.mentions.includes(meId)

  const run = (action: Promise<unknown>) => action.then(() => setError(''), (cause) => setError(errorMessage(cause)))
  const toggle = (emoji: string) => {
    const mine = message.reactions.find((r) => r.emoji === emoji)?.users.includes(meId) ?? false
    void run(api.react(message.channel_id, message.id, emoji, !mine))
  }

  const canDelete = own || canManage
  const cancelPress = () => {
    window.clearTimeout(pressTimer.current)
    setPressing(false)
  }
  const onTouchStart = () => {
    if (editing) return
    setPressing(true)
    pressTimer.current = window.setTimeout(() => {
      setPressing(false)
      setSheetOpen(true)
    }, 450)
  }
  const fromSheet = (action: () => void) => () => {
    setSheetOpen(false)
    action()
  }

  const body = editing ? <Editor message={message} onDone={() => setEditing(false)} /> : (
    <>
      {message.content ? (
        <p className="whitespace-pre-wrap break-words text-[0.9375rem] leading-6 text-fog">
          <Content content={message.content} people={people} />
          {message.edited_at ? <span className="ml-1 text-[0.65rem] text-greyple" title={new Date(message.edited_at).toLocaleString('pt-BR')}>(editada)</span> : null}
        </p>
      ) : null}
      {message.attachments.length ? <div className="mt-1 flex flex-col gap-2">{message.attachments.map((a) => <AttachmentView key={a.id} attachment={a} />)}</div> : null}
      {message.reactions.length ? (
        <div className="mt-1 flex flex-wrap gap-1">
          {message.reactions.map((r) => {
            const mine = r.users.includes(meId)
            return (
              <button key={r.emoji} type="button" disabled={!canReact && !mine} onClick={() => toggle(r.emoji)} aria-pressed={mine} className={cn('flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-sm', mine ? 'border-blurple bg-blurple/20 text-snow' : 'border-transparent bg-surface-raised text-fog hover:border-white/20')}>
                <span>{r.emoji}</span><span className="text-xs font-semibold" data-numeric>{r.users.length}</span>
              </button>
            )
          })}
        </div>
      ) : null}
      {error ? <p className="text-xs text-destructive">{error}</p> : null}
    </>
  )

  return (
    <li
      onTouchStart={onTouchStart}
      onTouchMove={cancelPress}
      onTouchEnd={cancelPress}
      onTouchCancel={cancelPress}
      // Long-press opens the sheet; suppress the native menu so it doesn't compete.
      onContextMenu={(e) => { if (pressing || sheetOpen) e.preventDefault() }}
      className={cn('group relative -mx-4 px-4 py-0.5 hover:bg-black/10', pressing && 'select-none [-webkit-touch-callout:none]', !grouped && 'mt-4 pt-1 first:mt-1', highlighted && 'border-l-2 border-ember-orange bg-ember-orange/10 hover:bg-ember-orange/15')}>
      {grouped ? (
        <div className="flex gap-4">
          <time dateTime={message.created_at} className="w-10 shrink-0 pt-1 text-right text-[0.65rem] text-greyple opacity-0 group-hover:opacity-100">{TIME_FORMAT.format(at)}</time>
          <div className="min-w-0 flex-1">{body}</div>
        </div>
      ) : (
        <div className="flex gap-4">
          <button type="button" onClick={() => openProfile(message.author.id)} className="h-fit shrink-0"><Avatar userId={message.author.id} displayName={name} avatarUrl={message.author.avatar_url} size="medium" ringClass="ring-surface-content" /></button>
          <div className="min-w-0 flex-1">
            <p className="flex flex-wrap items-baseline gap-x-2"><button type="button" onClick={() => openProfile(message.author.id)} style={{ color }} className="text-sm font-semibold text-snow hover:underline">{name}</button><time dateTime={message.created_at} className="text-[0.68rem] text-greyple">{DATE_FORMAT.format(at)} às {TIME_FORMAT.format(at)}</time></p>
            {body}
          </div>
        </div>
      )}

      {editing ? null : (
        <div className="absolute -top-3 right-4 hidden items-center rounded-md border border-white/10 bg-surface-secondary shadow-sm group-hover:flex has-[[data-state=open]]:flex">
          {canReact ? <ReactionPicker onPick={toggle} /> : null}
          {own ? <Button variant="ghost" size="icon-xs" aria-label="Editar mensagem" onClick={() => setEditing(true)}><Pencil aria-hidden /></Button> : null}
          {canDelete ? <Button variant="ghost" size="icon-xs" aria-label="Excluir mensagem" onClick={() => setConfirming(true)} className="hover:text-destructive"><Trash2 aria-hidden /></Button> : null}
        </div>
      )}

      <Sheet open={sheetOpen} onOpenChange={setSheetOpen}>
        <SheetContent side="bottom" showCloseButton={false} className="gap-1 rounded-t-xl p-3 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
          <SheetTitle className="sr-only">Ações da mensagem</SheetTitle>
          <SheetDescription className="sr-only">Mensagem de {name}</SheetDescription>
          {canReact ? (
            <div className="mb-2 flex justify-between">
              {EMOJIS.map((emoji) => <button key={emoji} type="button" onClick={fromSheet(() => toggle(emoji))} aria-label={`Reagir com ${emoji}`} className="rounded-full p-2 text-2xl active:bg-surface-hover">{emoji}</button>)}
            </div>
          ) : null}
          {message.content ? <Button variant="ghost" className="justify-start" onClick={fromSheet(() => void navigator.clipboard.writeText(message.content).catch(() => setError('Não foi possível copiar o texto.')))}><Copy aria-hidden />Copiar texto</Button> : null}
          {own ? <Button variant="ghost" className="justify-start" onClick={fromSheet(() => setEditing(true))}><Pencil aria-hidden />Editar</Button> : null}
          {canDelete ? <Button variant="ghost" className="justify-start text-destructive" onClick={fromSheet(() => setConfirming(true))}><Trash2 aria-hidden />Apagar</Button> : null}
        </SheetContent>
      </Sheet>

      <Dialog open={confirming} onOpenChange={setConfirming}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>Excluir mensagem</DialogTitle>
            <DialogDescription>Tem certeza de que quer excluir esta mensagem? Não dá para desfazer.</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <DialogClose asChild><Button variant="ghost">Cancelar</Button></DialogClose>
            <Button variant="destructive" onClick={() => { setConfirming(false); void run(api.deleteMessage(message.channel_id, message.id)) }}>Excluir</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </li>
  )
}
