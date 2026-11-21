import { describe, expect, it } from 'vitest'
import type { Message } from '@/lib/wire'
import { mentionQuery, splitMentions, startsNewGroup } from './format'

const msg = (authorId: string, created_at: string) =>
  ({ id: created_at, author: { id: authorId }, created_at }) as Message

describe('startsNewGroup', () => {
  it('groups the same author within 5 minutes', () => {
    expect(startsNewGroup(msg('a', '2026-01-01T10:04:00'), msg('a', '2026-01-01T10:00:00'))).toBe(false)
  })
  it('splits on first message, other author, long gap or new day', () => {
    expect(startsNewGroup(msg('a', '2026-01-01T10:00:00'), undefined)).toBe(true)
    expect(startsNewGroup(msg('b', '2026-01-01T10:01:00'), msg('a', '2026-01-01T10:00:00'))).toBe(true)
    expect(startsNewGroup(msg('a', '2026-01-01T10:06:00'), msg('a', '2026-01-01T10:00:00'))).toBe(true)
    expect(startsNewGroup(msg('a', '2026-01-02T00:01:00'), msg('a', '2026-01-01T23:59:00'))).toBe(true)
  })
})

describe('splitMentions', () => {
  it('extracts mentions between text', () => {
    expect(splitMentions('oi <@u-1>, e <@u2>!')).toEqual(['oi ', { userId: 'u-1' }, ', e ', { userId: 'u2' }, '!'])
  })
  it('leaves plain text alone', () => {
    expect(splitMentions('sem menção <@>')).toEqual(['sem menção <@>'])
    expect(splitMentions('')).toEqual([])
  })
})

describe('mentionQuery', () => {
  it('finds the @word before the caret', () => {
    expect(mentionQuery('oi @fu', 6)).toEqual({ start: 3, query: 'fu' })
    expect(mentionQuery('@', 1)).toEqual({ start: 0, query: '' })
  })
  it('ignores emails and finished mentions', () => {
    expect(mentionQuery('a@b', 3)).toBeNull()
    expect(mentionQuery('<@id> x', 7)).toBeNull()
    expect(mentionQuery('@fu bar', 7)).toBeNull()
  })
})
