// The built-in SFU (apps/server/internal/media/pion), over plain RTCPeerConnections.
// Two connections, so offers never collide: we offer on `pub` whenever what we send changes,
// the server offers on `sub` whenever what we receive changes. No trickle ICE: SDPs go out complete.
import type { TrackName } from '@/lib/wire'
import { preferH264, type Encoding } from './presets'
import { nameTracks, parseOfferTracks } from './sdp'
import type { RemoteTrack, SendOp, TransportEvents, VoiceTransport } from './transport'

const GATHER_TIMEOUT_MS = 3_000
const ANSWER_TIMEOUT_MS = 10_000

/** Resolves once the local description has all its candidates (or after a timeout, with what it has). */
const gathered = (pc: RTCPeerConnection) => new Promise<void>((resolve) => {
  if (pc.iceGatheringState === 'complete') return resolve()
  const done = () => {
    clearTimeout(timer)
    pc.removeEventListener('icegatheringstatechange', check)
    resolve()
  }
  const check = () => { if (pc.iceGatheringState === 'complete') done() }
  const timer = setTimeout(done, GATHER_TIMEOUT_MS)
  pc.addEventListener('icegatheringstatechange', check)
})

const applyEncoding = async (sender: RTCRtpSender, encoding: Encoding | undefined) => {
  if (!encoding) return
  const params = sender.getParameters()
  if (!params.encodings?.length) return // not negotiated yet: addTransceiver's sendEncodings already has it
  params.encodings = params.encodings.map((e) => ({ ...e, ...encoding }))
  await sender.setParameters(params)
}

const preferHardwareCodec = (transceiver: RTCRtpTransceiver) => {
  if (!('setCodecPreferences' in transceiver)) return
  try {
    transceiver.setCodecPreferences(preferH264(RTCRtpSender.getCapabilities('video')?.codecs ?? []))
  } catch {
    // The browser keeps its default order; still works, maybe with a software codec.
  }
}

const message = (cause: unknown) => (cause instanceof Error ? cause.message : String(cause))

export const createPionTransport = (send: SendOp, events: TransportEvents): VoiceTransport => {
  let closed = false
  let pub: RTCPeerConnection | null = null
  let sub: RTCPeerConnection | null = null
  const published = new Map<TrackName, RTCRtpTransceiver>()
  let pubQueue = Promise.resolve()
  let subQueue = Promise.resolve()
  let onAnswer: ((sdp: string) => void) | null = null

  const reportState = () => {
    if (closed) return
    const states = [pub, sub].flatMap((pc) => (pc ? [pc.connectionState] : []))
    events.onState(states.includes('failed') ? 'failed' : states.every((s) => s === 'connected') ? 'connected' : 'connecting')
  }

  // The server's SDPs carry its own candidates (public IP, one UDP port): no STUN/TURN needed.
  const peer = () => {
    const pc = new RTCPeerConnection()
    pc.addEventListener('connectionstatechange', reportState)
    return pc
  }

  const offer = async () => {
    if (closed || !pub) return
    await pub.setLocalDescription()
    await gathered(pub)
    if (closed) return
    const answer = new Promise<string>((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('o servidor não respondeu à oferta de mídia')), ANSWER_TIMEOUT_MS)
      onAnswer = (sdp) => {
        clearTimeout(timer)
        resolve(sdp)
      }
    })
    const tracks = nameTracks([...published].map(([name, t]) => ({ mid: t.mid, name })))
    send('rtc_offer', { sdp: pub.localDescription?.sdp, tracks })
    const sdp = await answer
    if (!closed) await pub.setRemoteDescription({ type: 'answer', sdp })
  }

  // Serialized: a new offer only after the previous answer was applied.
  const negotiate = () => {
    pubQueue = pubQueue.then(offer).catch((cause) => { if (!closed) events.onError(`Falha ao negociar o envio: ${message(cause)}`) })
    return pubQueue
  }

  const answer = async (sdp: string) => {
    if (closed) return
    if (!sub) {
      sub = peer()
      reportState()
    }
    await sub.setRemoteDescription({ type: 'offer', sdp })
    await sub.setLocalDescription()
    await gathered(sub)
    if (closed) return
    send('rtc_answer', { sdp: sub.localDescription?.sdp })
    // Map by mid from the offer itself: MediaStreamTrack.id is not reliable across browsers.
    const owners = parseOfferTracks(sdp)
    const tracks: RemoteTrack[] = []
    for (const t of sub.getTransceivers()) {
      const owner = t.mid ? owners.get(t.mid) : undefined
      if (owner) tracks.push({ ...owner, track: t.receiver.track })
    }
    events.onRemoteTracks(tracks)
  }

  return {
    publish: async (name, track, encoding) => {
      if (closed) return
      const current = published.get(name)
      if (!track) {
        if (!current) return
        published.delete(name)
        current.stop() // the m-section goes inactive and the SFU stops forwarding it
        return negotiate()
      }
      if (current) {
        if (current.sender.track !== track) await current.sender.replaceTrack(track)
        return applyEncoding(current.sender, encoding)
      }
      if (!pub) {
        pub = peer()
        reportState()
      }
      const transceiver = pub.addTransceiver(track, { direction: 'sendonly', ...(encoding ? { sendEncodings: [encoding] } : {}) })
      if (track.kind === 'video') preferHardwareCodec(transceiver)
      published.set(name, transceiver)
      return negotiate()
    },
    subscribe: (userId, track, on) => void send('rtc_subscribe', { user_id: userId, track, on }),
    handle: (e) => {
      if (closed) return
      if (e.t === 'RTC_ANSWER') {
        onAnswer?.(e.d.sdp)
        onAnswer = null
      } else if (e.t === 'RTC_OFFER') {
        const sdp = e.d.sdp
        subQueue = subQueue.then(() => answer(sdp)).catch((cause) => { if (!closed) events.onError(`Falha ao negociar a recepção: ${message(cause)}`) })
      }
    },
    close: () => {
      closed = true
      onAnswer = null
      pub?.close()
      sub?.close()
    },
  }
}
