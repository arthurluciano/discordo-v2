// How call media travels. The server picks the implementation per call (VOICE_READY's connection.kind);
// everything above this interface (capture, UI) is the same for all of them.
import type { GatewayEvent, TrackName } from '@/lib/wire'
import { createPionTransport } from './pion'
import type { Encoding } from './presets'

export type RemoteTrack = { userId: string; name: TrackName; track: MediaStreamTrack }

export type TransportState = 'connecting' | 'connected' | 'failed'

export type TransportEvents = {
  /** The full list of what we receive, every time it changes. */
  onRemoteTracks: (tracks: RemoteTrack[]) => void
  onState: (state: TransportState) => void
  /** A negotiation failed; the call may still work. */
  onError: (message: string) => void
}

export type SendOp = (op: string, d?: unknown) => boolean

export interface VoiceTransport {
  /** Starts, replaces (same name) or, with null, stops one of our published tracks.
   * Passing the track already published only updates its encoding. */
  publish: (name: TrackName, track: MediaStreamTrack | null, encoding?: Encoding) => Promise<void>
  /** Starts or stops receiving someone's video (audio is always received). */
  subscribe: (userId: string, track: 'camera' | 'screen', on: boolean) => void
  /** Call signaling from the gateway (RTC_OFFER, RTC_ANSWER, ...). */
  handle: (event: GatewayEvent) => void
  /** Closes the connections. Does not stop the local tracks (the caller owns them) nor leave the call. */
  close: () => void
}

/** Add a `livekit` entry here (a module implementing VoiceTransport) to support that server configuration. */
const TRANSPORTS: Record<string, (send: SendOp, events: TransportEvents) => VoiceTransport> = {
  pion: createPionTransport,
}

/** Null when this client does not know the kind. */
export const createTransport = (kind: string, send: SendOp, events: TransportEvents): VoiceTransport | null =>
  Object.hasOwn(TRANSPORTS, kind) ? TRANSPORTS[kind]!(send, events) : null
