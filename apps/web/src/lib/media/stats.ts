// What the browser's encoder is doing for a video we send, from RTCRtpSender.getStats().

export type EncoderStats = {
  /** `encoderImplementation`, e.g. "ExternalEncoder", "libvpx" or "SimulcastEncoderAdapter (…)". */
  encoder: string | null
  /** `powerEfficientEncoder` (Chrome): true means a hardware (GPU) encoder. */
  hardware: boolean | null
  /** Of the largest layer being sent. */
  width: number | null
  height: number | null
  fps: number | null
  /** Bits per second over all layers since the previous sample; null on the first one. */
  bitrate: number | null
  bytesSent: number
  timestamp: number
}

type Outbound = {
  type: string
  bytesSent?: number
  timestamp: number
  frameWidth?: number
  frameHeight?: number
  framesPerSecond?: number
  encoderImplementation?: string
  powerEfficientEncoder?: boolean
}

/** Sums the outbound-rtp entries of one sender (one per simulcast layer). */
export const encoderStats = (report: RTCStatsReport, previous: EncoderStats | null): EncoderStats => {
  const s: EncoderStats = { encoder: null, hardware: null, width: null, height: null, fps: null, bitrate: null, bytesSent: 0, timestamp: 0 }
  report.forEach((o: Outbound) => {
    if (o.type !== 'outbound-rtp') return
    s.bytesSent += o.bytesSent ?? 0
    s.timestamp = Math.max(s.timestamp, o.timestamp)
    s.encoder ??= o.encoderImplementation ?? null
    if (o.powerEfficientEncoder !== undefined) s.hardware = s.hardware === true || o.powerEfficientEncoder
    if (o.frameWidth && o.frameWidth > (s.width ?? 0)) {
      s.width = o.frameWidth
      s.height = o.frameHeight ?? null
      s.fps = o.framesPerSecond ?? null
    }
  })
  const elapsed = previous ? (s.timestamp - previous.timestamp) / 1000 : 0
  if (previous && elapsed > 0) s.bitrate = Math.max(0, ((s.bytesSent - previous.bytesSent) * 8) / elapsed)
  return s
}

export const encoderLabel = ({ hardware, encoder }: EncoderStats) => {
  const what = hardware === true ? 'GPU' : hardware === false ? 'CPU' : 'desconhecida'
  return `Codificação: ${what}${encoder ? ` (${encoder})` : ''}`
}

export const encoderDetails = ({ width, height, fps, bitrate }: EncoderStats) =>
  [
    width && height ? `${width}×${height}` : null,
    fps ? `${Math.round(fps)} fps` : null,
    bitrate !== null ? `${(bitrate / 1_000_000).toFixed(1).replace('.', ',')} Mbps` : null,
  ].filter(Boolean).join(' · ') || 'Sem dados ainda'
