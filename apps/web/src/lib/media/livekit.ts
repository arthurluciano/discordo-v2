// A LiveKit server (apps/server/internal/media/livekit): we connect with the URL and token from VOICE_READY;
// nothing goes over the gateway.
import {
  ConnectionState,
  Room,
  RoomEvent,
  Track,
  VideoQuality,
  type LocalTrackPublication,
  type RemoteParticipant,
  type RemoteTrackPublication,
  type TrackPublishOptions,
  type VideoCodec,
} from 'livekit-client'
import type { MediaConnection, TrackName } from '@/lib/wire'
import { probeFor, videoCodecs } from './codecs'
import type { Encoding } from './presets'
import type { Quality, RemoteTrack, SendOp, TransportEvents, VoiceTransport } from './transport'

const SOURCES = {
  mic: Track.Source.Microphone,
  camera: Track.Source.Camera,
  screen: Track.Source.ScreenShare,
  'screen-audio': Track.Source.ScreenShareAudio,
} as const satisfies Record<TrackName, Track.Source>

export const sourceOf = (name: TrackName): Track.Source => SOURCES[name]

export const nameOf = (source: Track.Source): TrackName | undefined =>
  (Object.keys(SOURCES) as TrackName[]).find((name) => SOURCES[name] === source)

const LIVEKIT_CODECS = ['h264', 'vp8', 'vp9', 'av1'] as const

/** The best codec this browser can send (GPU first, see codecs.ts) that LiveKit takes; VP8 is its own default. */
const bestCodec = async (track: MediaStreamTrack, encoding: Encoding | undefined): Promise<VideoCodec> => {
  for (const c of await videoCodecs(probeFor(track, encoding?.maxBitrate))) {
    const name = LIVEKIT_CODECS.find((n) => c.mimeType.toLowerCase() === `video/${n}`)
    if (name) return name
  }
  return 'vp8'
}

const publishOptions = async (name: TrackName, track: MediaStreamTrack, encoding: Encoding | undefined): Promise<TrackPublishOptions> => {
  const options: TrackPublishOptions = { source: SOURCES[name] }
  if (name === 'mic' || name === 'screen-audio') return encoding ? { ...options, audioPreset: { maxBitrate: encoding.maxBitrate } } : options
  // VP9/AV1 go out as SVC; LiveKit then also sends a VP8 backup for viewers that cannot decode them.
  const video = { ...options, simulcast: true, videoCodec: await bestCodec(track, encoding) }
  if (name === 'camera') return { ...video, ...(encoding && { videoEncoding: encoding }) }
  return { ...video, ...(encoding && { screenShareEncoding: encoding }) }
}

const QUALITIES: Record<Quality, VideoQuality> = {
  auto: VideoQuality.HIGH, // with adaptiveStream off LiveKit has no auto mode; the SFU still drops layers under congestion
  high: VideoQuality.HIGH,
  medium: VideoQuality.MEDIUM,
  low: VideoQuality.LOW,
}

const message = (cause: unknown) => (cause instanceof Error ? cause.message : String(cause))

export const createLiveKitTransport = (_send: SendOp, events: TransportEvents, connection: MediaConnection): VoiceTransport => {
  let closed = false
  // adaptiveStream stays off: the UI renders the raw MediaStreamTracks rather than track.attach(), so LiveKit
  // would think no one is watching and pause the video. Video is subscribed explicitly instead.
  const room = new Room({ adaptiveStream: false, dynacast: true, stopLocalTrackOnUnpublish: false })
  const published = new Map<TrackName, { pub: LocalTrackPublication; encoding: string }>()
  const wants = new Map<string, Quality>() // `${userId}/${name}` of the video we asked for
  let queue = Promise.resolve()

  const emit = () => {
    if (closed) return
    const tracks: RemoteTrack[] = []
    for (const p of room.remoteParticipants.values()) {
      for (const pub of p.trackPublications.values()) {
        const name = nameOf(pub.source)
        if (name && pub.track) tracks.push({ userId: p.identity, name, track: pub.track.mediaStreamTrack })
      }
    }
    events.onRemoteTracks(tracks)
  }

  const apply = (pub: RemoteTrackPublication, p: RemoteParticipant) => {
    const name = nameOf(pub.source)
    if (!name) return
    const quality = wants.get(`${p.identity}/${name}`)
    pub.setSubscribed(pub.kind === Track.Kind.Audio || quality !== undefined)
    if (quality) pub.setVideoQuality(QUALITIES[quality])
  }

  room
    .on(RoomEvent.TrackPublished, apply)
    .on(RoomEvent.TrackSubscribed, emit)
    .on(RoomEvent.TrackUnsubscribed, emit)
    .on(RoomEvent.ParticipantDisconnected, emit)
    .on(RoomEvent.ConnectionStateChanged, (s) => {
      if (closed) return
      events.onState(s === ConnectionState.Connected ? 'connected' : s === ConnectionState.Disconnected ? 'failed' : 'connecting')
    })

  const connected = (connection.url && connection.token
    ? room.connect(connection.url, connection.token, { autoSubscribe: false })
    : Promise.reject(new Error('o servidor não enviou o endereço e a credencial'))
  ).then(() => {
    // TrackPublished only fires for tracks published after we joined.
    for (const p of room.remoteParticipants.values()) p.trackPublications.forEach((pub) => apply(pub, p))
  })
  const ready = connected.then(() => true, (cause) => {
    if (!closed) {
      events.onError(`Falha ao conectar à mídia: ${message(cause)}`)
      events.onState('failed')
    }
    return false
  })

  const publish = async (name: TrackName, track: MediaStreamTrack | null, encoding?: Encoding) => {
    if (!(await ready) || closed) return
    const local = room.localParticipant
    const current = published.get(name)
    const key = JSON.stringify(encoding ?? null)
    // LiveKit sets the encodings when publishing, so a new one means publishing again.
    if (current && (!track || current.encoding !== key)) {
      published.delete(name)
      if (current.pub.track) await local.unpublishTrack(current.pub.track, false)
    } else if (current && track) {
      if (current.pub.track?.mediaStreamTrack !== track) await current.pub.track?.replaceTrack(track, true)
      return
    }
    if (track) published.set(name, { pub: await local.publishTrack(track, await publishOptions(name, track, encoding)), encoding: key })
  }

  return {
    // Serialized so a quick publish/unpublish pair applies in order; failures are reported, not thrown.
    publish: (name, track, encoding) => {
      queue = queue.then(() => publish(name, track, encoding)).catch((cause) => {
        if (!closed) events.onError(`Falha ao transmitir: ${message(cause)}`)
      })
      return queue
    },
    subscribe: (userId, track, on, quality = 'auto') => {
      const key = `${userId}/${track}`
      if (on) wants.set(key, quality)
      else wants.delete(key)
      const p = room.remoteParticipants.get(userId)
      const pub = p?.getTrackPublication(SOURCES[track])
      if (p && pub) apply(pub, p)
    },
    handle: () => {},
    getStats: async (name) => (await published.get(name)?.pub.track?.sender?.getStats()) ?? null,
    close: () => {
      closed = true
      void room.disconnect(false) // the caller owns the local tracks
    },
  }
}
