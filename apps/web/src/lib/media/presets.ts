// Screen share quality presets.
import { isCodecChoice, type CodecChoice } from './codecs'

export const RESOLUTIONS = {
  '480p': { label: '480p', width: 854, height: 480, mbps: 1.5 },
  '720p': { label: '720p', width: 1280, height: 720, mbps: 3 },
  '1080p': { label: '1080p', width: 1920, height: 1080, mbps: 6 },
  '1440p': { label: '1440p', width: 2560, height: 1440, mbps: 10 },
  '2160p': { label: '4K', width: 3840, height: 2160, mbps: 20 },
} as const

export type Resolution = keyof typeof RESOLUTIONS
export type ScreenPreset = { resolution: Resolution; fps: 30 | 60; codec?: CodecChoice }

export const DEFAULT_SCREEN_PRESET: ScreenPreset = { resolution: '1080p', fps: 30 }

export const isScreenPreset = (v: unknown): v is ScreenPreset =>
  typeof v === 'object' && v !== null && 'resolution' in v && 'fps' in v &&
  typeof v.resolution === 'string' && v.resolution in RESOLUTIONS && (v.fps === 30 || v.fps === 60) &&
  (!('codec' in v) || v.codec === undefined || isCodecChoice(v.codec))

export const presetLabel = (p: ScreenPreset) => `${RESOLUTIONS[p.resolution].label} ${p.fps} fps`

/** Capture constraints: never above the preset (the browser downscales larger sources). */
export const screenConstraints = ({ resolution, fps }: ScreenPreset): MediaTrackConstraints => {
  const { width, height } = RESOLUTIONS[resolution]
  return { width: { ideal: width, max: width }, height: { ideal: height, max: height }, frameRate: { ideal: fps, max: fps } }
}

/** `codec` only matters for video; absent means 'auto'. */
export type Encoding = { maxBitrate: number; maxFramerate?: number; codec?: CodecChoice }

/** 60 fps gets ~1.5x the bitrate of 30 fps at the same resolution. */
export const screenEncoding = ({ resolution, fps, codec }: ScreenPreset): Encoding => ({
  maxBitrate: Math.round(RESOLUTIONS[resolution].mbps * (fps === 60 ? 1.5 : 1) * 1_000_000),
  maxFramerate: fps,
  ...(codec && codec !== 'auto' ? { codec } : {}),
})

/** Text stays sharp at 30 fps ('detail' keeps resolution under pressure); 60 fps is for motion (keeps the frame rate). */
export const screenContentHint = ({ fps }: ScreenPreset) => (fps === 60 ? 'motion' : 'detail')

export const CAMERA_CONSTRAINTS: MediaTrackConstraints = { width: { ideal: 1280 }, height: { ideal: 720 }, frameRate: { ideal: 30 } }
export const CAMERA_ENCODING: Encoding = { maxBitrate: 2_500_000, maxFramerate: 30 }
export const SCREEN_AUDIO_ENCODING: Encoding = { maxBitrate: 128_000 }

/** Simulcast layers under the full one (rid `h`), lowest first: ~360p and ~720p, at their own bitrates. */
const LOWER_LAYERS = [
  { rid: 'l', height: 360, mbps: 0.6 },
  { rid: 'm', height: 720, mbps: RESOLUTIONS['720p'].mbps },
] as const

/** What to send for a video of this height: the full encoding plus the lower layers at least 1.5x smaller
 * (so a 480p video sends a single encoding). Without a height, a single encoding. */
export const sendEncodings = ({ maxBitrate, maxFramerate }: Encoding, height: number | undefined): RTCRtpEncodingParameters[] => {
  const top = { maxBitrate, ...(maxFramerate ? { maxFramerate } : {}) }
  const layers = LOWER_LAYERS.filter((l) => height && l.height * 1.5 <= height)
  if (!height || layers.length === 0) return [top]
  const motion = maxFramerate === 60 ? 1.5 : 1
  return [
    ...layers.map((l) => ({
      ...top,
      rid: l.rid,
      scaleResolutionDownBy: height / l.height,
      maxBitrate: Math.min(maxBitrate, Math.round(l.mbps * motion * 1_000_000)),
    })),
    { ...top, rid: 'h' },
  ]
}

/** A new encoding for what is already being sent: the full layer (rid `h`, or the only one) takes its bitrate,
 * every layer its frame rate. The layer count cannot change without renegotiating, so lower layers keep theirs. */
export const updateEncodings = (current: RTCRtpEncodingParameters[], { maxBitrate, maxFramerate }: Encoding) =>
  current.map((e) => ({
    ...e,
    ...(maxFramerate ? { maxFramerate } : {}),
    ...(!e.rid || e.rid === 'h' ? { maxBitrate } : {}),
  }))
