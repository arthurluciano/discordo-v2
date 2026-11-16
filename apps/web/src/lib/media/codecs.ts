// Which video codec to send: whatever the browser has, GPU (hardware) encoders first, CPU last.

type Codec = { mimeType: string; sdpFmtpLine?: string }

/** Within a tier, cheapest to encode and most widely decoded first: viewers must decode what we pick.
 * H.265 is left out (few browsers decode it); RTX/RED/FEC are not codecs and keep their place at the end. */
export const CODECS = { h264: 'H.264', vp8: 'VP8', vp9: 'VP9', av1: 'AV1' } as const
export type VideoCodecId = keyof typeof CODECS
/** 'auto' follows the GPU-first order; a codec puts that one first (the rest stay as fallbacks). */
export type CodecChoice = 'auto' | VideoCodecId

export const isCodecChoice = (v: unknown): v is CodecChoice => v === 'auto' || (typeof v === 'string' && v in CODECS)

const ORDER = Object.keys(CODECS).map((id) => `video/${id}`)

export type Probe = { width: number; height: number; framerate: number; bitrate: number }

/** Codecs (lowercase MIME types) the browser says it would encode power-efficiently, i.e. on the GPU, at this size. */
export const hardwareCodecs = async (probe: Probe): Promise<Set<string>> => {
  const hw = new Set<string>()
  if (!navigator.mediaCapabilities?.encodingInfo) return hw
  await Promise.all(ORDER.map(async (contentType) => {
    try {
      const info = await navigator.mediaCapabilities.encodingInfo({ type: 'webrtc', video: { contentType, ...probe } })
      if (info.supported && info.powerEfficient) hw.add(contentType)
    } catch {
      // Unknown to this browser: it stays in the CPU tier.
    }
  }))
  return hw
}

/** Sends GPU codecs first, then the rest by ORDER; H.264 packetization-mode=1 before 0. */
export const orderCodecs = <C extends Codec>(codecs: readonly C[], hardware: ReadonlySet<string>, choice: CodecChoice = 'auto'): C[] => {
  const rank = (c: C) => {
    const mime = c.mimeType.toLowerCase()
    const i = ORDER.indexOf(mime)
    if (i < 0) return 1000
    if (mime === `video/${choice}`) return -1
    const mode0 = mime === 'video/h264' && !/packetization-mode=1/.test(c.sdpFmtpLine ?? '') ? 0.5 : 0
    return (hardware.has(mime) ? 0 : ORDER.length) + i + mode0
  }
  return [...codecs].sort((a, b) => rank(a) - rank(b))
}

/** Probe for a video track: its own size, or 720p30 when the browser does not say. */
export const probeFor = (track: MediaStreamTrack, maxBitrate: number | undefined): Probe => {
  const s = track.getSettings?.() ?? {}
  return { width: s.width ?? 1280, height: s.height ?? 720, framerate: s.frameRate ?? 30, bitrate: maxBitrate ?? 2_500_000 }
}

const capabilities = () => globalThis.RTCRtpSender?.getCapabilities?.('video')?.codecs ?? []

/** The codecs this browser can send, best first. */
export const videoCodecs = async (probe: Probe, choice: CodecChoice = 'auto') =>
  orderCodecs(capabilities(), await hardwareCodecs(probe), choice)

/** The codecs of CODECS this browser can send. */
export const sendableCodecs = (): VideoCodecId[] => {
  const mimes = new Set(capabilities().map((c) => c.mimeType.toLowerCase()))
  return (Object.keys(CODECS) as VideoCodecId[]).filter((id) => mimes.has(`video/${id}`))
}
