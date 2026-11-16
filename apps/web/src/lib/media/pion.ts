// The built-in SFU (apps/server/internal/media/pion), over plain RTCPeerConnections.
// Two connections, so offers never collide: we offer on `pub` whenever what we send changes,
// the server offers on `sub` whenever what we receive changes. No trickle ICE: SDPs go out complete.
import type { TrackName } from '@/lib/wire'
import { probeFor, videoCodecs } from './codecs'
import { sendEncodings, updateEncodings, type Encoding } from './presets'
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
  params.encodings = updateEncodings(params.encodings, encoding)
  await sender.setParameters(params)
}

const setCodecs = (transceiver: RTCRtpTransceiver, codecs: RTCRtpCodec[]) => {
  try {
    transceiver.setCodecPreferences?.(codecs)
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
  const chosen = new Map<TrackName, string>() // the codec choice each video was published with
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
      const choice = encoding?.codec ?? 'auto'
      // The SFU forwards in the codec a track started with, so another codec means publishing it again.
      const recodec = track?.kind === 'video' && published.has(name) && chosen.get(name) !== choice
      // Probed before anything else so the rest stays synchronous (no offer can slip in between).
      const codecs = track?.kind === 'video' && (!published.has(name) || recodec) ? await videoCodecs(probeFor(track, encoding?.maxBitrate), choice) : null
      if (closed) return
      let current = published.get(name)
      if (!track || recodec) {
        if (!current) return
        published.delete(name)
        current.stop() // the m-section goes inactive and the SFU stops forwarding it
        current = undefined
        if (!track) return negotiate()
      }
      if (current) {
        if (current.sender.track !== track) await current.sender.replaceTrack(track)
        return applyEncoding(current.sender, encoding)
      }
      if (!pub) {
        pub = peer()
        reportState()
      }
      // Video is simulcast (rids h/m/l, see presets) so the SFU can give each viewer the layer it can take.
      const encodings = encoding ? sendEncodings(encoding, track.kind === 'video' ? track.getSettings?.().height : undefined) : undefined
      let transceiver: RTCRtpTransceiver
      try {
        transceiver = pub.addTransceiver(track, { direction: 'sendonly', ...(encodings ? { sendEncodings: encodings } : {}) })
      } catch (cause) {
        if (!encoding || encodings?.length === 1) throw cause
        transceiver = pub.addTransceiver(track, { direction: 'sendonly', sendEncodings: sendEncodings(encoding, undefined) }) // no simulcast here
      }
      if (codecs) setCodecs(transceiver, codecs)
      chosen.set(name, choice)
      published.set(name, transceiver)
      return negotiate()
    },
    subscribe: (userId, track, on, quality) => void send('rtc_subscribe', { user_id: userId, track, on, ...(quality ? { quality } : {}) }),
    getStats: async (name) => (await published.get(name)?.sender.getStats()) ?? null,
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
