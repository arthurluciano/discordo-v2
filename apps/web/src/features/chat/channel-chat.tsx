// The conversation of a guild text channel or a DM.
import { useCallback, useEffect, useMemo, useState, type DragEvent, type ReactNode } from 'react'
import { ArrowLeft, Hash, Paperclip } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { api, errorMessage } from '@/lib/api'
import { ALL, has, Permission } from '@/lib/permissions'
import { loadOlder, openChannel, useStore } from '@/lib/store'
import { displayName, useMe } from '../identity/users'
import { Composer, useUploads } from './composer'
import type { People } from './message-item'
import { Transcript } from './transcript'
import { TypingIndicator } from './typing-indicator'
import { roleColors } from './role-color'

type ChannelChatProps = {
  channelId: string
  /** Undefined for DMs. */
  guildId?: string
  title: string
  topic?: string
  /** Extra header buttons (e.g. start a call, group members). */
  actions?: ReactNode
  /** Mobile: back to the navigation pane. */
  onBack: () => void
}

const Chat = ({ channelId, guildId, title }: { channelId: string; guildId: string | undefined; title: string }) => {
  const me = useMe()
  const guild = useStore((s) => (guildId ? s.guilds.find((g) => g.id === guildId) : undefined))
  const dm = useStore((s) => (guildId ? undefined : s.dms.find((d) => d.id === channelId)))
  const log = useStore((s) => s.messages[channelId])
  const ready = useStore((s) => s.status === 'ready')
  const [failed, setFailed] = useState<string | null>(null)
  const [dragging, setDragging] = useState(false)
  const uploads = useUploads(channelId)

  const fetchPage = useCallback((before?: string) => api.messages(channelId, before), [channelId])

  // A READY empties `messages`, so this re-opens the channel after a reconnect too.
  useEffect(() => {
    if (!log && ready && !failed) openChannel(channelId, fetchPage).catch((cause) => setFailed(errorMessage(cause)))
  }, [channelId, fetchPage, log, ready, failed])

  const permissions = guild ? guild.channels.find((c) => c.id === channelId)?.permissions ?? 0 : ALL

  const mentionable = useMemo(() => {
    const people = guild
      ? guild.members.map((m) => ({ user: m.user, name: displayName(m.user, m.nickname) }))
      : [...(dm?.recipients ?? []), ...(me ? [me] : [])].map((user) => ({ user, name: displayName(user) }))
    return people.sort((a, b) => a.name.localeCompare(b.name, 'pt-BR'))
  }, [guild, dm, me])

  const colors = useMemo(() => (guild ? roleColors(guild) : undefined), [guild])
  const people: People = useMemo(() => new Map(mentionable.map((p) => [p.user.id, p.name])), [mentionable])

  const canAttach = has(permissions, Permission.AttachFiles)
  const loadMore = useCallback(() => void loadOlder(channelId, fetchPage).catch(() => {}), [channelId, fetchPage])

  const onDrop = (event: DragEvent) => {
    event.preventDefault()
    setDragging(false)
    if (canAttach) uploads.add(event.dataTransfer.files)
  }
  const onDragOver = (event: DragEvent) => {
    if (!canAttach || !event.dataTransfer.types.includes('Files')) return
    event.preventDefault()
    setDragging(true)
  }

  if (!me) return null

  return (
    <div className="relative flex min-h-0 flex-1 flex-col" onDragOver={onDragOver} onDragLeave={(e) => !e.currentTarget.contains(e.relatedTarget as Node) && setDragging(false)} onDrop={onDrop}>
      {failed ? (
        <div className="grid flex-1 place-items-center text-center">
          <div>
            <p className="mb-3 text-sm text-fog">Não foi possível carregar as mensagens: {failed}</p>
            <Button variant="secondary" size="sm" onClick={() => setFailed(null)}>Tentar de novo</Button>
          </div>
        </div>
      ) : (
        <Transcript log={log} title={title} isDM={!guildId} meId={me.id} people={people} colors={colors} canReact={has(permissions, Permission.AddReactions)} canManage={has(permissions, Permission.ManageMessages)} onLoadOlder={loadMore} />
      )}
      <TypingIndicator channelId={channelId} meId={me.id} people={people} />
      <Composer channelId={channelId} placeholder={guildId ? `Conversar em #${title}` : `Conversar com ${title}`} canSend={has(permissions, Permission.SendMessages)} canAttach={canAttach} uploads={uploads} mentionable={mentionable} />
      {dragging ? (
        <div className="pointer-events-none absolute inset-2 grid place-items-center rounded-xl border-2 border-dashed border-blurple bg-surface-content/90 glass:bg-(--canvas)/90 glass:backdrop-blur-md">
          <p className="flex items-center gap-2 font-semibold text-snow"><Paperclip aria-hidden />Solte para anexar</p>
        </div>
      ) : null}
    </div>
  )
}

export const ChannelChat = ({ channelId, guildId, title, topic, actions, onBack }: ChannelChatProps) => (
  <section className="flex h-full min-h-0 min-w-0 flex-1 flex-col bg-surface-content" aria-label={guildId ? `#${title}` : title}>
    <header className="flex h-12 shrink-0 items-center gap-2 border-b border-white/5 px-2 md:px-4">
      <Button variant="ghost" size="icon-sm" onClick={onBack} className="md:hidden" aria-label="Voltar"><ArrowLeft aria-hidden /></Button>
      {guildId ? <Hash aria-hidden className="size-5 shrink-0 text-greyple" /> : null}
      <h1 className="truncate font-bold text-snow">{title}</h1>
      {topic ? <><span className="mx-2 hidden h-6 w-px bg-white/10 sm:block" /><p className="hidden min-w-0 flex-1 truncate text-xs text-greyple sm:block" title={topic}>{topic}</p></> : <span className="flex-1" />}
      {actions ? <div className="flex shrink-0 items-center gap-0.5">{actions}</div> : null}
    </header>
    <Chat key={channelId} channelId={channelId} guildId={guildId} title={title} />
  </section>
)
