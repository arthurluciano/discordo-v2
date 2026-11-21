import { describe, expect, it } from 'vitest'
import { dispatch, getState, initialState, openChannel, REACTION_MS, reduce, type State } from './store'
import type { GatewayEvent, Guild, Message, User } from './wire'

const alice: User = { id: 'a', username: 'alice', display_name: 'Alice', avatar_url: '' }

const bob: User = { id: 'b', username: 'bob', display_name: 'Bob', avatar_url: '' }

const guild: Guild = {
  id: 'g',
  name: 'Oficina',
  icon_url: '',
  banner_url: '',
  owner_id: 'a',
  permissions: 0,
  roles: [],
  channels: [
    { id: 'c', guild_id: 'g', type: 0, name: 'geral', topic: '', position: 0, overwrites: [], permissions: 0 },
  ],
  members: [
    { user: alice, nickname: '', roles: [], joined_at: '', online: true },
    { user: bob, nickname: '', roles: [], joined_at: '', online: false },
  ],
}

const message = (id: string, author = alice): Message => ({
  id,
  channel_id: 'c',
  author,
  content: id,
  created_at: '',
  edited_at: null,
  mentions: [],
  mention_everyone: false,
  attachments: [],
  reactions: [],
})

const ready = (): State =>
  reduce(initialState, {
    t: 'READY',
    d: { user: alice, guilds: [guild], dms: [], relationships: [], voice_states: [] },
  })

const withLog = (s: State, list: Message[]): State => ({ ...s, messages: { c: { list, complete: true } } })

describe('reduce', () => {
  it('replaces everything on READY', () => {
    const s = ready()
    expect(s.status).toBe('ready')
    expect(
      reduce(withLog(s, [message('1')]), {
        t: 'READY',
        d: { user: alice, guilds: [], dms: [], relationships: [], voice_states: [] },
      }).messages,
    ).toEqual({})
  })

  it('appends new messages once, and only to channels already loaded', () => {
    expect(reduce(ready(), { t: 'MESSAGE_CREATE', d: message('1') }).messages).toEqual({})
    const s = reduce(withLog(ready(), [message('1')]), { t: 'MESSAGE_CREATE', d: message('2') })
    expect(reduce(s, { t: 'MESSAGE_CREATE', d: message('2') }).messages.c?.list.map((m) => m.id)).toEqual(['1', '2'])
  })

  it('toggles reactions per user', () => {
    let s = withLog(ready(), [message('1')])
    const add = (user: string) => ({
      t: 'REACTION_ADD' as const,
      d: { channel_id: 'c', message_id: '1', user_id: user, emoji: '👍' },
    })
    s = reduce(reduce(reduce(s, add('a')), add('b')), add('a'))
    expect(s.messages.c?.list[0]?.reactions).toEqual([{ emoji: '👍', users: ['b', 'a'] }])
    s = reduce(reduce(s, { ...add('a'), t: 'REACTION_REMOVE' }), { ...add('b'), t: 'REACTION_REMOVE' })
    expect(s.messages.c?.list[0]?.reactions).toEqual([])
  })

  it('updates a user everywhere they appear', () => {
    const renamed = { ...bob, display_name: 'Roberto', avatar_url: '/images/x' }
    const s = reduce(withLog(ready(), [message('1', bob)]), { t: 'USER_UPDATE', d: renamed })
    expect(s.guilds[0]?.members[1]?.user).toEqual(renamed)
    expect(s.messages.c?.list[0]?.author).toEqual(renamed)
  })

  it('drops messages of channels that are no longer visible', () => {
    const s = reduce(withLog(ready(), [message('1')]), { t: 'GUILD_SYNC', d: { ...guild, channels: [] } })
    expect(s.messages).toEqual({})
  })

  it('tracks who is in a call and who left', () => {
    const joined = reduce(ready(), {
      t: 'VOICE_STATE_UPDATE',
      d: { user_id: 'b', channel_id: 'v', muted: false, deafened: false, tracks: ['mic'] },
    })
    expect(joined.voice.b?.channel_id).toBe('v')
    expect(
      reduce(joined, {
        t: 'VOICE_STATE_UPDATE',
        d: { user_id: 'b', channel_id: '', muted: false, deafened: false, tracks: [] },
      }).voice,
    ).toEqual({})
  })

  it('moves someone in a world, and ignores who is not in a call', () => {
    const joined = reduce(ready(), {
      t: 'VOICE_STATE_UPDATE',
      d: { user_id: 'b', channel_id: 'w', muted: false, deafened: false, tracks: [], x: 1, y: 2, zone: 'cinema' },
    })
    const moved = reduce(joined, { t: 'WORLD_MOVE', d: { channel_id: 'w', user_id: 'b', x: 30, y: 40 } })
    expect(moved.voice.b).toMatchObject({ x: 30, y: 40, zone: 'cinema' })
    expect(reduce(moved, { t: 'WORLD_MOVE', d: { channel_id: 'w', user_id: 'zz', x: 1, y: 1 } })).toBe(moved)
  })

  it('floats world reactions and forgets the old ones', () => {
    const first = reduce(ready(), { t: 'WORLD_REACT', d: { channel_id: 'w', user_id: 'b', emoji: '🔥' } }, 1000)
    const later = reduce(
      first,
      { t: 'WORLD_REACT', d: { channel_id: 'w', user_id: 'c', emoji: '🎉' } },
      1000 + REACTION_MS,
    )
    expect(later.worldReactions).toEqual([{ user_id: 'c', emoji: '🎉', at: 1000 + REACTION_MS }])
  })

  it('expires typing and clears it when the message arrives', () => {
    const typing = reduce(withLog(ready(), []), { t: 'TYPING_START', d: { channel_id: 'c', user_id: 'b' } }, 1000)
    expect(typing.typing.c?.b).toBe(9000)
    expect(reduce(typing, { t: 'MESSAGE_CREATE', d: message('1', bob) }).typing.c?.b).toBeUndefined()
  })
})

describe('openChannel', () => {
  it('drops a page requested before a reconnect', async () => {
    const readyEvent: GatewayEvent = {
      t: 'READY',
      d: { user: alice, guilds: [guild], dms: [], relationships: [], voice_states: [] },
    }
    dispatch(readyEvent)
    let resolve!: (page: Message[]) => void
    const opening = openChannel(
      'c',
      () =>
        new Promise((r) => {
          resolve = r
        }),
    )
    dispatch(readyEvent) // reconnected while the first page was in flight
    resolve([message('old')])
    await opening
    expect(getState().messages.c).toBeUndefined()
    await openChannel('c', async () => [message('new')]) // and opening it again is not blocked
    expect(getState().messages.c?.list.map((m) => m.id)).toEqual(['new'])
  })
})
