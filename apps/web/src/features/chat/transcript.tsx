import { useEffect, useLayoutEffect, useRef } from 'react'
import { Hash, MessageCircle } from 'lucide-react'
import { Spinner } from '@/components/ui/spinner'
import { isLoadingChannel, type MessageLog } from '@/lib/store'
import { sameDay, startsNewGroup } from './format'
import { MessageItem, type People } from './message-item'

const DAY_FORMAT = new Intl.DateTimeFormat('pt-BR', { day: 'numeric', month: 'long', year: 'numeric' })
const NEAR = 80 // px from an edge that still counts as "at" it

type TranscriptProps = {
  log: MessageLog | undefined
  title: string
  isDM: boolean
  meId: string
  people: People
  canReact: boolean
  canManage: boolean
  onLoadOlder: () => void
}

export const Transcript = ({ log, title, isDM, meId, people, canReact, canManage, onLoadOlder }: TranscriptProps) => {
  const scroller = useRef<HTMLDivElement>(null)
  const content = useRef<HTMLOListElement>(null)
  const atBottom = useRef(true)
  const previous = useRef({ first: '', height: 0 })
  const messages = log?.list ?? []
  const first = messages[0]?.id ?? ''

  // Keep the view where it was when older messages are prepended; otherwise follow the bottom if we were there.
  useLayoutEffect(() => {
    const el = scroller.current
    if (!el) return
    if (previous.current.first && first !== previous.current.first && !atBottom.current) el.scrollTop += el.scrollHeight - previous.current.height
    else if (atBottom.current) el.scrollTop = el.scrollHeight
    previous.current = { first, height: el.scrollHeight }
    if (log?.complete === false && log.list.length && el.scrollHeight <= el.clientHeight) onLoadOlder()
  })

  // Images, videos and the typing line change the height after render.
  useEffect(() => {
    const el = scroller.current
    if (!el || !content.current) return
    const observer = new ResizeObserver(() => {
      if (atBottom.current) el.scrollTop = el.scrollHeight
      previous.current.height = el.scrollHeight
    })
    observer.observe(content.current)
    return () => observer.disconnect()
  }, [])

  const onScroll = () => {
    const el = scroller.current!
    atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < NEAR
    if (el.scrollTop < NEAR * 3) onLoadOlder()
  }

  return (
    <div ref={scroller} onScroll={onScroll} className="min-h-0 flex-1 overflow-y-auto bg-surface-content">
      <ol ref={content} className="flex min-h-full w-full flex-col justify-end px-4 pb-5 pt-4">
        {isLoadingChannel(log) || !log?.complete ? (
          <li className="grid place-items-center py-6"><Spinner /></li>
        ) : (
          <li className="flex items-end py-8">
            <div className="max-w-xl">
              <span className="grid size-16 place-items-center rounded-full bg-surface-raised">{isDM ? <MessageCircle className="size-8 text-fog" aria-hidden /> : <Hash className="size-8 text-fog" aria-hidden />}</span>
              <h2 className="mt-4 text-3xl font-bold text-snow">{isDM ? title : `Boas-vindas a #${title}!`}</h2>
              <p className="mt-2 text-fog">{isDM ? `Este é o começo da sua conversa com ${title}.` : `Este é o começo do canal #${title}.`}</p>
            </div>
          </li>
        )}
        {messages.map((message, index) => {
          const prev = messages[index - 1]
          const newDay = !prev || !sameDay(prev.created_at, message.created_at)
          return [
            newDay ? (
              <li key={`day-${message.id}`} role="separator" className="my-4 flex items-center gap-2 text-[0.7rem] font-semibold text-greyple">
                <span className="h-px flex-1 bg-white/10" />{DAY_FORMAT.format(new Date(message.created_at))}<span className="h-px flex-1 bg-white/10" />
              </li>
            ) : null,
            <MessageItem key={message.id} message={message} grouped={!startsNewGroup(message, prev)} meId={meId} people={people} canReact={canReact} canManage={canManage} />,
          ]
        })}
      </ol>
    </div>
  )
}
