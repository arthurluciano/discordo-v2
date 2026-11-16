// The client's copy of the server state, fed by the gateway (READY replaces it, events patch it).
// `reduce` is pure; components read through `useStore` with a selector.
import { useSyncExternalStore } from 'react'
import type { DM, GatewayEvent, Guild, Message, Relationship, User, VoiceState } from './wire'

export type MessageLog = {
  /** Oldest first. */
  list: Message[]
  /** No older messages left to load. */
  complete: boolean
}

export type State = {
  /** `offline`: reconnecting; `signed-out`: the session is gone. */
  status: 'connecting' | 'ready' | 'offline' | 'signed-out'
  me: User | null
  guilds: Guild[]
  dms: DM[]
  relationships: Relationship[]
  /** By user ID: everyone in a call we can see. */
  voice: Record<string, VoiceState>
  /** channel ID -> user ID -> when their "typing" expires (ms). */
  typing: Record<string, Record<string, number>>
  /** Only for channels opened since the last READY. */
  messages: Record<string, MessageLog>
}

export const initialState: State = {
  status: 'connecting', me: null, guilds: [], dms: [], relationships: [], voice: {}, typing: {}, messages: {},
}

const TYPING_MS = 8000

const upsert = <T extends { id: string }>(list: T[], item: T) =>
  list.some((x) => x.id === item.id) ? list.map((x) => (x.id === item.id ? item : x)) : [...list, item]

const mapGuild = (s: State, guildId: string, fn: (g: Guild) => Guild): State => ({
  ...s, guilds: s.guilds.map((g) => (g.id === guildId ? fn(g) : g)),
})

const mapMessages = (s: State, channelId: string, fn: (list: Message[]) => Message[]): State => {
  const log = s.messages[channelId]
  return log ? { ...s, messages: { ...s.messages, [channelId]: { ...log, list: fn(log.list) } } } : s
}

const react = (m: Message, emoji: string, userId: string, on: boolean): Message => {
  const others = m.reactions.filter((r) => r.emoji !== emoji)
  const users = (m.reactions.find((r) => r.emoji === emoji)?.users ?? []).filter((u) => u !== userId)
  if (on) users.push(userId)
  return { ...m, reactions: users.length ? [...others, { emoji, users }] : others }
}

const patchUser = (s: State, u: User): State => ({
  ...s,
  me: s.me?.id === u.id ? u : s.me,
  guilds: s.guilds.map((g) => ({ ...g, members: g.members.map((m) => (m.user.id === u.id ? { ...m, user: u } : m)) })),
  dms: s.dms.map((d) => ({ ...d, recipients: d.recipients.map((r) => (r.id === u.id ? u : r)) })),
  relationships: s.relationships.map((r) => (r.user.id === u.id ? { ...r, user: u } : r)),
  messages: Object.fromEntries(Object.entries(s.messages).map(([c, log]) => [
    c, { ...log, list: log.list.map((m) => (m.author.id === u.id ? { ...m, author: u } : m)) },
  ])),
})

export const reduce = (s: State, e: GatewayEvent, now = Date.now()): State => {
  switch (e.t) {
    case 'READY':
      return {
        ...initialState, status: 'ready', me: e.d.user, guilds: e.d.guilds, dms: e.d.dms, relationships: e.d.relationships,
        voice: Object.fromEntries(e.d.voice_states.map((v) => [v.user_id, v])),
      }
    case 'GUILD_SYNC': {
      // Channels we can no longer see take their messages with them.
      const visible = new Set(e.d.channels.map((c) => c.id))
      const old = s.guilds.find((g) => g.id === e.d.id)
      const gone = old?.channels.filter((c) => !visible.has(c.id)).map((c) => c.id) ?? []
      const messages = { ...s.messages }
      gone.forEach((c) => delete messages[c])
      return { ...s, guilds: upsert(s.guilds, e.d), messages }
    }
    case 'GUILD_DELETE': {
      const guild = s.guilds.find((g) => g.id === e.d.id)
      const messages = { ...s.messages }
      guild?.channels.forEach((c) => delete messages[c.id])
      return { ...s, guilds: s.guilds.filter((g) => g.id !== e.d.id), messages }
    }
    case 'MEMBER_ADD':
      return mapGuild(s, e.d.guild_id, (g) => ({
        ...g, members: [...g.members.filter((m) => m.user.id !== e.d.member.user.id), e.d.member],
      }))
    case 'MEMBER_REMOVE':
      return mapGuild(s, e.d.guild_id, (g) => ({ ...g, members: g.members.filter((m) => m.user.id !== e.d.user_id) }))
    case 'PRESENCE_UPDATE':
      return {
        ...s,
        guilds: s.guilds.map((g) => ({
          ...g, members: g.members.map((m) => (m.user.id === e.d.user_id ? { ...m, online: e.d.online } : m)),
        })),
        relationships: s.relationships.map((r) => (r.user.id === e.d.user_id ? { ...r, online: e.d.online } : r)),
      }
    case 'USER_UPDATE':
      return patchUser(s, e.d)
    case 'MESSAGE_CREATE': {
      const typing = s.typing[e.d.channel_id]
      const after = typing?.[e.d.author.id] ? { ...s, typing: { ...s.typing, [e.d.channel_id]: { ...typing, [e.d.author.id]: 0 } } } : s
      return mapMessages(after, e.d.channel_id, (list) => (list.some((m) => m.id === e.d.id) ? list : [...list, e.d]))
    }
    case 'MESSAGE_UPDATE':
      return mapMessages(s, e.d.channel_id, (list) => list.map((m) => (m.id === e.d.id ? e.d : m)))
    case 'MESSAGE_DELETE':
      return mapMessages(s, e.d.channel_id, (list) => list.filter((m) => m.id !== e.d.id))
    case 'REACTION_ADD':
    case 'REACTION_REMOVE': {
      const on = e.t === 'REACTION_ADD'
      return mapMessages(s, e.d.channel_id, (list) =>
        list.map((m) => (m.id === e.d.message_id ? react(m, e.d.emoji, e.d.user_id, on) : m)))
    }
    case 'TYPING_START':
      return { ...s, typing: { ...s.typing, [e.d.channel_id]: { ...s.typing[e.d.channel_id], [e.d.user_id]: now + TYPING_MS } } }
    case 'DM_CREATE':
    case 'DM_UPDATE':
      return { ...s, dms: upsert(s.dms, e.d) }
    case 'DM_DELETE': {
      const messages = { ...s.messages }
      delete messages[e.d.id]
      return { ...s, dms: s.dms.filter((d) => d.id !== e.d.id), messages }
    }
    case 'RELATIONSHIP_UPDATE': {
      const others = s.relationships.filter((r) => r.user.id !== e.d.user.id)
      return { ...s, relationships: e.d.status === 0 ? others : [...others, e.d] }
    }
    case 'VOICE_STATE_UPDATE': {
      const voice = { ...s.voice }
      if (e.d.channel_id) voice[e.d.user_id] = e.d
      else delete voice[e.d.user_id]
      return { ...s, voice }
    }
    default:
      return s // call signaling is handled by the voice session, not stored
  }
}

// ---- the live store ----

let state = initialState
const listeners = new Set<() => void>()

export const getState = () => state

export const setState = (fn: (s: State) => State) => {
  state = fn(state)
  listeners.forEach((l) => l())
}

export const dispatch = (e: GatewayEvent) => setState((s) => reduce(s, e))

const subscribe = (listener: () => void) => {
  listeners.add(listener)
  return () => void listeners.delete(listener)
}

/** Reads part of the state. The selector must return something already in the state (or a
 * primitive): a new array or object on every call would re-render forever. Derive with useMemo. */
export const useStore = <T>(selector: (s: State) => T): T => useSyncExternalStore(subscribe, () => selector(state))

// ---- messages ----

const PAGE = 50
const loading = new Set<string>()

/** Loads the newest messages of a channel, once per channel (later ones arrive by the gateway).
 * While loading, the log exists but is empty and not `complete`, so messages arriving meanwhile are kept. */
export const openChannel = async (channelId: string, fetchPage: (before?: string) => Promise<Message[]>) => {
  if (state.messages[channelId] || loading.has(channelId)) return
  loading.add(channelId)
  setState((s) => ({ ...s, messages: { ...s.messages, [channelId]: { list: [], complete: false } } }))
  try {
    const page = (await fetchPage()).reverse()
    setState((s) => {
      const live = s.messages[channelId]?.list ?? []
      const list = [...page, ...live.filter((m) => !page.some((p) => p.id === m.id))]
      return { ...s, messages: { ...s.messages, [channelId]: { list, complete: page.length < PAGE } } }
    })
  } catch (cause) {
    setState((s) => {
      const messages = { ...s.messages }
      delete messages[channelId] // so that opening the channel again retries
      return { ...s, messages }
    })
    throw cause
  } finally {
    loading.delete(channelId)
  }
}

/** True while the first page of a channel is still loading. */
export const isLoadingChannel = (log: MessageLog | undefined) => !log || (!log.complete && log.list.length === 0)

/** Loads the page before the oldest message we have. */
export const loadOlder = async (channelId: string, fetchPage: (before?: string) => Promise<Message[]>) => {
  const log = state.messages[channelId]
  if (!log || log.complete || loading.has(channelId)) return
  loading.add(channelId)
  try {
    const page = await fetchPage(log.list[0]?.id)
    setState((s) => {
      const current = s.messages[channelId]
      if (!current) return s
      const older = page.reverse().filter((m) => !current.list.some((c) => c.id === m.id))
      return { ...s, messages: { ...s.messages, [channelId]: { list: [...older, ...current.list], complete: page.length < PAGE } } }
    })
  } finally {
    loading.delete(channelId)
  }
}

export const MESSAGE_PAGE = PAGE
