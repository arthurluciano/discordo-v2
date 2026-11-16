// Pure helpers for the home screens: DM titles and friend list filtering.
import { displayName } from '@/features/identity/users'
import { RelationshipStatus, type DM, type Relationship, type User } from '@/lib/wire'

/** The server lists the user among a DM's recipients; these are everyone else. */
export const otherRecipients = (dm: DM, meId: string | undefined): User[] => dm.recipients.filter((r) => r.id !== meId)

export const dmTitle = (dm: DM, meId: string | undefined) =>
  dm.name || otherRecipients(dm, meId).map((u) => displayName(u)).join(', ') || 'Grupo vazio'

export type FriendTab = 'online' | 'all' | 'pending' | 'blocked'

const inTab = (r: Relationship, tab: FriendTab) => {
  switch (tab) {
    case 'online': return r.status === RelationshipStatus.Friend && r.online
    case 'all': return r.status === RelationshipStatus.Friend
    case 'pending': return r.status === RelationshipStatus.Incoming || r.status === RelationshipStatus.Outgoing
    case 'blocked': return r.status === RelationshipStatus.Blocked
  }
}

/** Relationships shown under a tab, by name (incoming requests first in "pending"). */
export const friendsIn = (relationships: Relationship[], tab: FriendTab) =>
  relationships
    .filter((r) => inTab(r, tab))
    .sort((a, b) => a.status - b.status || displayName(a.user).localeCompare(displayName(b.user), 'pt-BR'))
