// The permission math, ported from apps/server/internal/permission. The server enforces it; the UI
// uses it only to hide what the user cannot do.
import type { Channel, Guild, Overwrite } from './wire'

export const Permission = {
  ViewChannel: 1 << 0,
  SendMessages: 1 << 1,
  ManageMessages: 1 << 2,
  AttachFiles: 1 << 3,
  AddReactions: 1 << 4,
  MentionEveryone: 1 << 5,
  Connect: 1 << 6,
  Speak: 1 << 7,
  Stream: 1 << 8,
  CreateInvite: 1 << 9,
  KickMembers: 1 << 10,
  BanMembers: 1 << 11,
  ManageChannels: 1 << 12,
  ManageRoles: 1 << 13,
  ManageGuild: 1 << 14,
  Administrator: 1 << 15,
} as const

export type PermissionName = keyof typeof Permission

export const ALL = (Permission.Administrator << 1) - 1

/** Bits a channel overwrite may touch. */
export const CHANNEL_SCOPED =
  Permission.ViewChannel | Permission.SendMessages | Permission.ManageMessages | Permission.AttachFiles |
  Permission.AddReactions | Permission.MentionEveryone | Permission.Connect | Permission.Speak | Permission.Stream |
  Permission.ManageChannels | Permission.ManageRoles

export const has = (set: number, bits: number) => (set & bits) === bits

/** Applies channel overwrites: @everyone, then the member's roles (allow wins), then the member. */
export const compute = (base: number, everyoneId: string, userId: string, roleIds: string[], overwrites: Overwrite[]) => {
  if (has(base, Permission.Administrator)) return ALL
  let p = base
  let roleAllow = 0, roleDeny = 0, memberAllow = 0, memberDeny = 0
  for (const o of overwrites) {
    if (o.is_member) {
      if (o.target_id === userId) [memberAllow, memberDeny] = [o.allow, o.deny]
    } else if (o.target_id === everyoneId) {
      p = (p & ~o.deny) | o.allow
    } else if (roleIds.includes(o.target_id)) {
      roleAllow |= o.allow
      roleDeny |= o.deny
    }
  }
  p = (p & ~roleDeny) | roleAllow
  return (p & ~memberDeny) | memberAllow
}

/** What `userId` may do in `channel` of `guild` (the guild-level permissions come from the snapshot). */
export const channelPermissions = (guild: Guild, channel: Channel, userId: string) => {
  const me = guild.members.find((m) => m.user.id === userId)
  return compute(guild.permissions, guild.id, userId, me?.roles ?? [], channel.overwrites)
}

/** The highest role position the user holds (@everyone is 0); the owner outranks everyone. */
export const topPosition = (guild: Guild, userId: string) => {
  if (guild.owner_id === userId) return Number.MAX_SAFE_INTEGER
  const roles = guild.members.find((m) => m.user.id === userId)?.roles ?? []
  return Math.max(0, ...guild.roles.filter((r) => roles.includes(r.id)).map((r) => r.position))
}
