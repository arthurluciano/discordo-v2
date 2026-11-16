// Pure helpers for the transcript and the composer.
import type { Message } from '@/lib/wire'

const GROUPING_WINDOW_MS = 5 * 60_000

export const sameDay = (a: string, b: string) => new Date(a).toDateString() === new Date(b).toDateString()

/** A message starts a new block (avatar + name) unless it follows the same author within 5 minutes on the same day. */
export const startsNewGroup = (message: Message, previous: Message | undefined) =>
  !previous ||
  previous.author.id !== message.author.id ||
  !sameDay(previous.created_at, message.created_at) ||
  Date.parse(message.created_at) - Date.parse(previous.created_at) > GROUPING_WINDOW_MS

/** Splits message text into plain strings and `<@id>` mentions. */
export const splitMentions = (content: string): (string | { userId: string })[] => {
  const parts: (string | { userId: string })[] = []
  let last = 0
  for (const match of content.matchAll(/<@([\w-]+)>/g)) {
    if (match.index > last) parts.push(content.slice(last, match.index))
    parts.push({ userId: match[1]! })
    last = match.index + match[0].length
  }
  if (last < content.length) parts.push(content.slice(last))
  return parts
}

/** The `@word` being typed right before the caret, if any. */
export const mentionQuery = (text: string, caret: number): { start: number; query: string } | null => {
  const match = /(?:^|\s)@([^\s@<>]*)$/.exec(text.slice(0, caret))
  return match ? { start: caret - match[1]!.length - 1, query: match[1]! } : null
}

export const formatSize = (bytes: number) =>
  bytes < 1024 ? `${bytes} B` : bytes < 1024 ** 2 ? `${(bytes / 1024).toFixed(1)} KB` : `${(bytes / 1024 ** 2).toFixed(1)} MB`
