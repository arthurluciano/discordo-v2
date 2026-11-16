// How call media travels. The server picks the implementation per call (VOICE_READY's connection.kind);
// everything above this interface (capture, UI) is the same for all of them.
import type { GatewayEvent, MediaConnection, TrackName } from '@/lib/wire'
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

/** Which simulcast layer of a video to receive; "auto" adapts to the connection. */
export type Quality = 'auto' | 'high' | 'medium' | 'low'

export interface VoiceTransport {
  /** Starts, replaces (same name) or, with null, stops one of our published tracks.
   * Passing the track already published only updates its encoding. */
  publish: (name: TrackName, track: MediaStreamTrack | null, encoding?: Encoding) => Promise<void>
  /** Starts or stops receiving someone's video (audio is always received). Subscribing again with another
   * quality only changes the layer received; transports without simulcast may ignore it. */
  subscribe: (userId: string, track: 'camera' | 'screen', on: boolean, quality?: Quality) => void
  /** Call signaling from the gateway (RTC_OFFER, RTC_ANSWER, ...). */
  handle: (event: GatewayEvent) => void
  /** The sender stats of one of our published tracks (for the encoder indicator); null if not published. Optional. */
  getStats?: (name: TrackName) => Promise<RTCStatsReport | null>
  /** Closes the connections. Does not stop the local tracks (the caller owns them) nor leave the call. */
  close: () => void
}

type Factory = (send: SendOp, events: TransportEvents, connection: MediaConnection) => VoiceTransport

/** Loads a transport's module on first use, queueing calls until then (calls keep their order). */
const lazy = (load: () => Promise<Factory>): Factory => (send, events, connection) => {
  let closed = false
  const ready = load().then(
    (create) => (closed ? null : create(send, events, connection)),
    () => {
      events.onError('Não foi possível carregar o módulo de mídia. Recarregue a página.')
      events.onState('failed')
      return null
    },
  )
  return {
    publish: (name, track, encoding) => ready.then((t) => t?.publish(name, track, encoding)),
    subscribe: (...args) => void ready.then((t) => t?.subscribe(...args)),
    handle: (event) => void ready.then((t) => t?.handle(event)),
    getStats: (name) => ready.then((t) => t?.getStats?.(name) ?? null),
    close: () => {
      closed = true
      void ready.then((t) => t?.close())
    },
  }
}

const TRANSPORTS: Record<string, Factory> = {
  pion: createPionTransport,
  // Most servers use pion: keep livekit-client out of the main bundle.
  livekit: lazy(() => import('./livekit').then((m) => m.createLiveKitTransport)),
}

/** Null when this client does not know the kind. */
export const createTransport = (connection: MediaConnection, send: SendOp, events: TransportEvents): VoiceTransport | null =>
  Object.hasOwn(TRANSPORTS, connection.kind) ? TRANSPORTS[connection.kind]!(send, events, connection) : null
