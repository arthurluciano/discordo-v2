import { useCallback, useMemo, useRef, useState, type ClipboardEvent, type FormEvent, type KeyboardEvent } from 'react'
import { FileText, Lock, Paperclip, SendHorizontal, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'
import { Avatar } from '@/design/avatar'
import { api, errorMessage } from '@/lib/api'
import { cn } from '@/lib/utils'
import type { Attachment, User } from '@/lib/wire'
import { formatSize, mentionQuery } from './format'

const MAX_UPLOAD = 25 * 1024 * 1024

const TYPING_EVERY_MS = 6000

type Upload = { key: number; file: File; attachment?: Attachment; error?: string }

let nextKey = 0

/** Files being attached to the next message. Lives in the chat so drops anywhere on it land here. */
export const useUploads = (channelId: string) => {
  const [items, setItems] = useState<Upload[]>([])
  const patch = (key: number, change: Partial<Upload>) =>
    setItems((list) => list.map((u) => (u.key === key ? { ...u, ...change } : u)))
  const add = useCallback(
    (files: Iterable<File>) => {
      for (const file of files) {
        const key = nextKey++
        if (file.size > MAX_UPLOAD) {
          setItems((list) => [...list, { key, file, error: 'Maior que 25 MB' }])
          continue
        }
        setItems((list) => [...list, { key, file }])
        api.uploadAttachment(channelId, file).then(
          (attachment) => patch(key, { attachment }),
          (cause) => patch(key, { error: errorMessage(cause) }),
        )
      }
    },
    [channelId],
  )
  const remove = (key: number) => setItems((list) => list.filter((u) => u.key !== key))

  return { items, add, remove, clear: () => setItems([]) }
}

export type Uploads = ReturnType<typeof useUploads>

type ComposerProps = {
  channelId: string
  placeholder: string
  canSend: boolean
  canAttach: boolean
  uploads: Uploads
  /** Who `@` can mention here. */
  mentionable: { user: User; name: string }[]
}

export const Composer = ({ channelId, placeholder, canSend, canAttach, uploads, mentionable }: ComposerProps) => {
  const [draft, setDraft] = useState('')
  const [caret, setCaret] = useState(0)
  const [highlight, setHighlight] = useState(0)
  const [dismissed, setDismissed] = useState(false)
  const [sending, setSending] = useState(false)
  const [error, setError] = useState('')
  const lastTyping = useRef(0)
  const input = useRef<HTMLTextAreaElement>(null)
  const filePicker = useRef<HTMLInputElement>(null)

  const query = dismissed ? null : mentionQuery(draft, caret)
  const suggestions = useMemo(() => {
    if (!query) return []
    const q = query.query.toLowerCase()
    return mentionable
      .filter((p) => p.name.toLowerCase().includes(q) || p.user.username.toLowerCase().includes(q))
      .slice(0, 8)
  }, [query, mentionable])

  if (!canSend) {
    return (
      <div className="shrink-0 px-4 pb-[max(1.25rem,env(safe-area-inset-bottom))]">
        <p className="flex items-center gap-2 rounded-lg bg-surface-raised px-4 py-3 text-sm text-greyple">
          <Lock aria-hidden className="size-4" />
          Você não tem permissão para enviar mensagens neste canal.
        </p>
      </div>
    )
  }

  const uploading = uploads.items.some((u) => !u.attachment && !u.error)
  const ready = uploads.items.filter((u) => u.attachment).map((u) => u.attachment!.id)
  const canSubmit = (draft.trim().length > 0 || ready.length > 0) && !uploading && !sending

  const submit = async (event?: FormEvent) => {
    event?.preventDefault()
    if (!canSubmit) return
    setSending(true)
    try {
      await api.sendMessage(channelId, draft.trim(), ready)
      setDraft('')
      setError('')
      uploads.clear()
      lastTyping.current = 0
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setSending(false)
    }
  }

  const insertMention = (userId: string) => {
    if (!query) return
    const text = `${draft.slice(0, query.start)}<@${userId}> `
    setDraft(text + draft.slice(caret))
    setCaret(text.length)
    requestAnimationFrame(() => input.current?.setSelectionRange(text.length, text.length))
  }

  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (suggestions.length) {
      if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
        event.preventDefault()
        const step = event.key === 'ArrowDown' ? 1 : -1
        setHighlight((h) => (h + step + suggestions.length) % suggestions.length)
        return
      }
      if (event.key === 'Enter' || event.key === 'Tab') {
        event.preventDefault()
        insertMention(suggestions[Math.min(highlight, suggestions.length - 1)]!.user.id)
        return
      }
      if (event.key === 'Escape') {
        setDismissed(true)
        return
      }
    }
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault()
      void submit()
    }
  }

  const onChange = (text: string, at: number) => {
    setDraft(text)
    setCaret(at)
    setHighlight(0)
    setDismissed(false)
    if (text && Date.now() - lastTyping.current > TYPING_EVERY_MS) {
      lastTyping.current = Date.now()
      api.typing(channelId).catch(() => {})
    }
  }

  const onPaste = (event: ClipboardEvent) => {
    if (!canAttach || !event.clipboardData.files.length) return
    event.preventDefault()
    uploads.add(event.clipboardData.files)
  }

  return (
    <form
      onSubmit={submit}
      className="relative shrink-0 bg-surface-content px-4 pb-[max(1.25rem,env(safe-area-inset-bottom))]"
    >
      {suggestions.length ? (
        <ul
          role="listbox"
          aria-label="Mencionar"
          className="absolute inset-x-4 bottom-full mb-1 overflow-hidden rounded-lg bg-popover p-1 shadow-md ring-1 ring-foreground/10"
        >
          {suggestions.map((p, i) => (
            <li key={p.user.id} role="option" aria-selected={i === highlight}>
              <button
                type="button"
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => insertMention(p.user.id)}
                onMouseEnter={() => setHighlight(i)}
                className={cn(
                  'flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm',
                  i === highlight && 'bg-surface-hover',
                )}
              >
                <Avatar
                  userId={p.user.id}
                  displayName={p.name}
                  avatarUrl={p.user.avatar_url}
                  ringClass="ring-popover"
                />
                <span className="truncate font-medium text-snow">{p.name}</span>
                <span className="truncate text-xs text-greyple">@{p.user.username}</span>
              </button>
            </li>
          ))}
        </ul>
      ) : null}

      <div className="rounded-lg bg-surface-raised focus-within:ring-2 focus-within:ring-blurple/25">
        {uploads.items.length ? (
          <ul className="flex flex-wrap gap-2 border-b border-white/5 p-2">
            {uploads.items.map((u) => (
              <li
                key={u.key}
                className={cn(
                  'flex max-w-56 items-center gap-2 rounded-md bg-surface-secondary py-1 pl-2 pr-1 text-xs',
                  u.error && 'ring-1 ring-destructive',
                )}
              >
                {u.attachment || u.error ? (
                  <FileText aria-hidden className="size-4 shrink-0 text-greyple" />
                ) : (
                  <Spinner className="shrink-0" />
                )}
                <span className="min-w-0">
                  <span className="block truncate text-snow">{u.file.name}</span>
                  <span className={u.error ? 'text-destructive' : 'text-greyple'}>
                    {u.error ?? formatSize(u.file.size)}
                  </span>
                </span>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  aria-label={`Remover ${u.file.name}`}
                  onClick={() => uploads.remove(u.key)}
                >
                  <X aria-hidden />
                </Button>
              </li>
            ))}
          </ul>
        ) : null}
        <div className="flex items-end gap-1 px-2 py-1">
          {canAttach ? (
            <>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label="Anexar arquivo"
                onClick={() => filePicker.current?.click()}
              >
                <Paperclip aria-hidden />
              </Button>
              <input
                ref={filePicker}
                type="file"
                multiple
                hidden
                onChange={(e) => {
                  uploads.add(e.target.files ?? [])
                  e.target.value = ''
                }}
              />
            </>
          ) : null}
          <label className="sr-only" htmlFor={`composer-${channelId}`}>
            {placeholder}
          </label>
          <Textarea
            ref={input}
            id={`composer-${channelId}`}
            rows={1}
            value={draft}
            onChange={(e) => onChange(e.target.value, e.target.selectionStart)}
            onSelect={(e) => setCaret(e.currentTarget.selectionStart)}
            onKeyDown={onKeyDown}
            onPaste={onPaste}
            placeholder={placeholder}
            className="max-h-48 min-h-10 flex-1 resize-none border-0 bg-transparent px-2 py-2.5 focus-visible:ring-0"
          />
          <Button type="submit" size="icon-sm" disabled={!canSubmit} aria-label="Enviar mensagem">
            <SendHorizontal aria-hidden />
          </Button>
        </div>
      </div>
      <p className="mt-1 px-1 text-[0.65rem] text-greyple">
        {error ? <span className="text-destructive">{error}</span> : 'Enter envia · Shift + Enter quebra a linha'}
      </p>
    </form>
  )
}
