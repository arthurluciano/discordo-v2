// Finding users anywhere in the state. Each returns an object already in the state (store-safe).
import { RelationshipStatus, type User } from '@/lib/wire'
import { useStore, type State } from '@/lib/store'

export const findUser = (s: State, userId: string): User | null => {
  if (s.me?.id === userId) return s.me
  for (const g of s.guilds) {
    const m = g.members.find((x) => x.user.id === userId)
    if (m) return m.user
  }
  for (const d of s.dms) {
    const r = d.recipients.find((x) => x.id === userId)
    if (r) return r
  }
  return s.relationships.find((r) => r.user.id === userId)?.user ?? null
}

/** Online if any guild or friendship says so (the server only tells us about people we share one with). */
export const isOnline = (s: State, userId: string) =>
  s.me?.id === userId ||
  s.guilds.some((g) => g.members.some((m) => m.user.id === userId && m.online)) ||
  s.relationships.some((r) => r.user.id === userId && r.status === RelationshipStatus.Friend && r.online)

export const useUser = (userId: string | null) => useStore((s) => (userId ? findUser(s, userId) : null))
export const useOnline = (userId: string) => useStore((s) => isOnline(s, userId))
export const useMe = () => useStore((s) => s.me)

/** The name to show for a user, in a guild (its nickname) or anywhere else. */
export const displayName = (user: User, nickname = '') => nickname || user.display_name || user.username
