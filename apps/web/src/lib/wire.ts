// What the server sends and accepts, mirroring docs/protocol.md (the Go types are the source of truth).
// Field names stay snake_case, exactly as on the wire.

export type User = { id: string; username: string; display_name: string; avatar_url: string }

export type Role = { id: string; guild_id: string; name: string; color: number; permissions: number; position: number }

export type Member = { user: User; nickname: string; roles: string[]; joined_at: string; online: boolean }

export type Overwrite = { target_id: string; is_member: boolean; allow: number; deny: number }

export const ChannelType = { Text: 0, Voice: 1, DM: 2, Group: 3 } as const

export type Channel = {
  id: string
  guild_id: string
  type: typeof ChannelType.Text | typeof ChannelType.Voice
  name: string
  topic: string
  position: number
  overwrites: Overwrite[]
}

/** A channel inside a guild snapshot: `permissions` are what we may do in it (computed by the server). */
export type GuildChannel = Channel & { permissions: number }

/** A guild as one member sees it: only the channels they can view; `permissions` are theirs. */
export type Guild = {
  id: string
  name: string
  icon_url: string
  banner_url: string
  owner_id: string
  permissions: number
  roles: Role[]
  channels: GuildChannel[]
  members: Member[]
}

export type Attachment = { id: string; filename: string; content_type: string; size: number }

export type Reaction = { emoji: string; users: string[] }

export type Message = {
  id: string
  channel_id: string
  author: User
  content: string
  created_at: string
  edited_at: string | null
  mentions: string[]
  mention_everyone: boolean
  attachments: Attachment[]
  reactions: Reaction[]
}

export type DM = {
  id: string
  type: typeof ChannelType.DM | typeof ChannelType.Group
  name: string
  recipients: User[]
}

export const RelationshipStatus = { None: 0, Friend: 1, Incoming: 2, Outgoing: 3, Blocked: 4 } as const

export type Relationship = { user: User; status: number; online: boolean }

/** Someone in a call. An empty `channel_id` means they left. `tracks` are what they publish. */
export type VoiceState = {
  user_id: string
  channel_id: string
  guild_id?: string
  muted: boolean
  deafened: boolean
  tracks: TrackName[]
}

export type TrackName = 'mic' | 'screen-audio' | 'camera' | 'screen'

/** How to reach the call's media: "pion" signals over the gateway; "livekit" connects to `url` with `token`. */
export type MediaConnection = { kind: string; url?: string; token?: string }

export type Invite = {
  code: string
  guild_id: string
  creator_id: string
  max_uses: number
  uses: number
  expires_at: string | null
  creator?: User
}

export type Ban = { user: User }

export type Ready = {
  user: User
  guilds: Guild[]
  dms: DM[]
  relationships: Relationship[]
  voice_states: VoiceState[]
}

/** Every gateway event the client handles, by type. */
export type GatewayEvents = {
  READY: Ready
  GUILD_SYNC: Guild
  GUILD_DELETE: { id: string }
  MEMBER_ADD: { guild_id: string; member: Member }
  MEMBER_REMOVE: { guild_id: string; user_id: string }
  PRESENCE_UPDATE: { user_id: string; online: boolean }
  USER_UPDATE: User
  MESSAGE_CREATE: Message
  MESSAGE_UPDATE: Message
  MESSAGE_DELETE: { id: string; channel_id: string }
  REACTION_ADD: { channel_id: string; message_id: string; user_id: string; emoji: string }
  REACTION_REMOVE: { channel_id: string; message_id: string; user_id: string; emoji: string }
  TYPING_START: { channel_id: string; user_id: string }
  DM_CREATE: DM
  DM_UPDATE: DM
  DM_DELETE: { id: string }
  RELATIONSHIP_UPDATE: Relationship
  VOICE_STATE_UPDATE: VoiceState
  VOICE_READY: { channel_id: string; connection: MediaConnection }
  VOICE_ERROR: { op: string; error: string }
  RTC_OFFER: { sdp: string }
  RTC_ANSWER: { sdp: string }
}

export type GatewayEvent = { [T in keyof GatewayEvents]: { t: T; d: GatewayEvents[T] } }[keyof GatewayEvents]
