// Where the user is: home (friends or a DM) or a guild channel. Kept apart from the server state
// because a READY must not move anyone. `content` says which pane a phone shows: the list
// (navigation) or what was picked in it.
import { create } from 'zustand'

export type Place = (
  | { kind: 'home'; dmId: string | null } // null: the friends screen
  | { kind: 'guild'; guildId: string; channelId: string | null }
) & { content: boolean }

export const usePlace = create<Place>()(() => ({ kind: 'home', dmId: null, content: false }))

const lastChannel = new Map<string, string>()

const set = (next: Place) => {
  if (next.kind === 'guild' && next.channelId) lastChannel.set(next.guildId, next.channelId)
  usePlace.setState(next, true)
}

export const navigate = {
  /** Home, showing the list of DMs (the friends screen beside it on wide screens). */
  home: () => set({ kind: 'home', dmId: null, content: false }),
  friends: () => set({ kind: 'home', dmId: null, content: true }),
  dm: (dmId: string) => set({ kind: 'home', dmId, content: true }),
  /** Opens a guild at the channel the user last had open there. */
  guild: (guildId: string) =>
    set({ kind: 'guild', guildId, channelId: lastChannel.get(guildId) ?? null, content: false }),
  channel: (guildId: string, channelId: string) => set({ kind: 'guild', guildId, channelId, content: true }),
}
