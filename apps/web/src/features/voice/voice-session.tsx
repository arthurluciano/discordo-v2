// CONTRACT (implemented by the voice area): the call the user is in, and how to join or leave one.
import { createContext, useContext, type ReactNode } from 'react'

export type VoiceApi = {
  /** The call we are in: a guild voice channel or a DM. */
  channelId: string | null
  connection: 'idle' | 'connecting' | 'connected' | 'failed'
  error: string | null
  join: (channelId: string) => void
  leave: () => void
  muted: boolean
  deafened: boolean
  setMuted: (muted: boolean) => void
  setDeafened: (deafened: boolean) => void
}

const idle: VoiceApi = {
  channelId: null, connection: 'idle', error: null, join: () => {}, leave: () => {},
  muted: false, deafened: false, setMuted: () => {}, setDeafened: () => {},
}

const VoiceContext = createContext<VoiceApi>(idle)

/** Owns the call for the whole session: navigating never interrupts it. */
export const VoiceProvider = ({ children }: { children: ReactNode }) => (
  <VoiceContext.Provider value={idle}>{children}</VoiceContext.Provider>
)

export const useVoice = () => useContext(VoiceContext)
