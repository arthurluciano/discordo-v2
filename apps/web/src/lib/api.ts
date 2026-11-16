// The REST API. Same origin as the app, authenticated by the session cookie (see docs/protocol.md).
import type { Attachment, Ban, Channel, DM, Guild, Invite, Message, Role, User, UserProfile } from './wire'

export type ProfileInput = Partial<
  Pick<UserProfile, 'display_name' | 'avatar_url' | 'bio' | 'pronouns' | 'banner_color'>
>

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

/** Human text for any error thrown by the API layer or anything else. */
export const errorMessage = (cause: unknown) => (cause instanceof Error ? cause.message : 'Algo deu errado.')

const request = async <T>(method: string, path: string, body?: unknown): Promise<T> => {
  const init: RequestInit = { method, credentials: 'same-origin' }
  if (body instanceof FormData)
    init.body = body // the browser sets the multipart boundary itself
  else if (body !== undefined) {
    init.body = JSON.stringify(body)
    init.headers = { 'content-type': 'application/json' }
  }
  let response: Response
  try {
    response = await fetch(path, init)
  } catch {
    throw new ApiError(0, 'Sem conexão com o servidor.')
  }
  if (response.status === 204) return undefined as T
  const payload: unknown = await response.json().catch(() => null)
  if (!response.ok) {
    const message =
      typeof payload === 'object' && payload && 'error' in payload ? String(payload.error) : `Erro ${response.status}`
    throw new ApiError(response.status, message)
  }

  return payload as T
}

const upload = <T>(path: string, file: File) => {
  const body = new FormData()
  body.append('file', file)

  return request<T>('POST', path, body)
}

const enc = encodeURIComponent

export const api = {
  me: () => request<User>('GET', '/users/@me'),
  updateProfile: (input: ProfileInput) => request<User>('PATCH', '/users/@me', input),
  profile: (userId: string) => request<UserProfile>('GET', `/users/${userId}/profile`),
  logout: () => request<void>('POST', '/auth/logout'),
  /** Stores a picture for an avatar, icon or banner and returns its URL. */
  uploadImage: async (file: File) => (await upload<{ url: string }>('/images', file)).url,

  createGuild: (name: string) => request<Guild>('POST', '/guilds', { name }),
  updateGuild: (guildId: string, input: { name?: string; icon_url?: string; banner_url?: string }) =>
    request<void>('PATCH', `/guilds/${guildId}`, input),
  deleteGuild: (guildId: string) => request<void>('DELETE', `/guilds/${guildId}`),
  leaveGuild: (guildId: string) => request<void>('DELETE', `/guilds/${guildId}/members/@me`),
  kick: (guildId: string, userId: string) => request<void>('DELETE', `/guilds/${guildId}/members/${userId}`),
  ban: (guildId: string, userId: string) => request<void>('PUT', `/guilds/${guildId}/bans/${userId}`),
  unban: (guildId: string, userId: string) => request<void>('DELETE', `/guilds/${guildId}/bans/${userId}`),
  bans: (guildId: string) => request<Ban[]>('GET', `/guilds/${guildId}/bans`),
  setMemberRole: (guildId: string, userId: string, roleId: string, on: boolean) =>
    request<void>(on ? 'PUT' : 'DELETE', `/guilds/${guildId}/members/${userId}/roles/${roleId}`),

  createRole: (guildId: string, input: Partial<Pick<Role, 'name' | 'color' | 'permissions' | 'position'>>) =>
    request<Role>('POST', `/guilds/${guildId}/roles`, input),
  updateRole: (
    guildId: string,
    roleId: string,
    input: Partial<Pick<Role, 'name' | 'color' | 'permissions' | 'position'>>,
  ) => request<Role>('PATCH', `/guilds/${guildId}/roles/${roleId}`, input),
  /** Swaps the role with the one right above (or below) it. */
  moveRole: (guildId: string, roleId: string, up: boolean) =>
    request<void>('POST', `/guilds/${guildId}/roles/${roleId}/move`, { up }),
  deleteRole: (guildId: string, roleId: string) => request<void>('DELETE', `/guilds/${guildId}/roles/${roleId}`),

  /** `max_age` in seconds; 0 (or absent) means no limit, for both. */
  createInvite: (guildId: string, input: { max_uses?: number; max_age?: number } = {}) =>
    request<Invite>('POST', `/guilds/${guildId}/invites`, input),
  joinInvite: (code: string) => request<Guild>('POST', `/invites/${enc(code)}`),
  /** The guild's unexpired invites, with their creators. */
  invites: (guildId: string) => request<Invite[]>('GET', `/guilds/${guildId}/invites`),
  revokeInvite: (code: string) => request<void>('DELETE', `/invites/${enc(code)}`),

  createChannel: (
    guildId: string,
    input: { name: string; type?: Channel['type']; topic?: string; position?: number },
  ) => request<Channel>('POST', `/guilds/${guildId}/channels`, input),
  updateChannel: (channelId: string, input: { name?: string; topic?: string; position?: number }) =>
    request<Channel>('PATCH', `/channels/${channelId}`, input),
  deleteChannel: (channelId: string) => request<void>('DELETE', `/channels/${channelId}`),
  setOverwrite: (channelId: string, targetId: string, input: { is_member: boolean; allow: number; deny: number }) =>
    request<void>('PUT', `/channels/${channelId}/overwrites/${targetId}`, input),
  deleteOverwrite: (channelId: string, targetId: string) =>
    request<void>('DELETE', `/channels/${channelId}/overwrites/${targetId}`),

  /** Newest first, at most `limit` (100), older than `before` when given. */
  messages: (channelId: string, before?: string, limit = 50) =>
    request<Message[]>('GET', `/channels/${channelId}/messages?limit=${limit}${before ? `&before=${before}` : ''}`),
  sendMessage: (channelId: string, content: string, attachments: string[] = []) =>
    request<Message>('POST', `/channels/${channelId}/messages`, { content, attachments }),
  editMessage: (channelId: string, messageId: string, content: string) =>
    request<Message>('PATCH', `/channels/${channelId}/messages/${messageId}`, { content }),
  deleteMessage: (channelId: string, messageId: string) =>
    request<void>('DELETE', `/channels/${channelId}/messages/${messageId}`),
  react: (channelId: string, messageId: string, emoji: string, on: boolean) =>
    request<void>(on ? 'PUT' : 'DELETE', `/channels/${channelId}/messages/${messageId}/reactions/${enc(emoji)}`),
  typing: (channelId: string) => request<void>('POST', `/channels/${channelId}/typing`),
  uploadAttachment: (channelId: string, file: File) => upload<Attachment>(`/channels/${channelId}/attachments`, file),

  /** Sends a friend request (or accepts theirs). */
  addFriend: (username: string) => request<void>('POST', '/relationships', { username }),
  acceptFriend: (userId: string) => request<void>('PUT', `/relationships/${userId}`),
  /** Unfriends, declines, cancels or unblocks. */
  removeRelationship: (userId: string) => request<void>('DELETE', `/relationships/${userId}`),
  block: (userId: string) => request<void>('PUT', `/blocks/${userId}`),

  openDM: (recipientId: string) => request<DM>('POST', '/dms', { recipient_id: recipientId }),
  createGroup: (recipientIds: string[], name?: string) =>
    request<DM>('POST', '/dms/groups', { recipient_ids: recipientIds, name }),
  addToGroup: (channelId: string, userIds: string[]) =>
    request<void>('POST', `/dms/${channelId}/recipients`, { recipient_ids: userIds }),
  leaveGroup: (channelId: string) => request<void>('DELETE', `/dms/${channelId}/recipients/@me`),
}

/** Where an attachment can be fetched (the session cookie authorizes it). */
export const attachmentUrl = (a: Attachment) => `/attachments/${a.id}`
