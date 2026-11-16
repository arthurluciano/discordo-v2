// Screen share quality presets and video codec preference.

export const RESOLUTIONS = {
  '480p': { label: '480p', width: 854, height: 480, mbps: 1.5 },
  '720p': { label: '720p', width: 1280, height: 720, mbps: 3 },
  '1080p': { label: '1080p', width: 1920, height: 1080, mbps: 6 },
  '1440p': { label: '1440p', width: 2560, height: 1440, mbps: 10 },
  '2160p': { label: '4K', width: 3840, height: 2160, mbps: 20 },
} as const

export type Resolution = keyof typeof RESOLUTIONS
export type ScreenPreset = { resolution: Resolution; fps: 30 | 60 }

export const DEFAULT_SCREEN_PRESET: ScreenPreset = { resolution: '1080p', fps: 30 }

export const isScreenPreset = (v: unknown): v is ScreenPreset =>
  typeof v === 'object' && v !== null && 'resolution' in v && 'fps' in v &&
  typeof v.resolution === 'string' && v.resolution in RESOLUTIONS && (v.fps === 30 || v.fps === 60)

export const presetLabel = (p: ScreenPreset) => `${RESOLUTIONS[p.resolution].label} ${p.fps} fps`

/** Capture constraints: never above the preset (the browser downscales larger sources). */
export const screenConstraints = ({ resolution, fps }: ScreenPreset): MediaTrackConstraints => {
  const { width, height } = RESOLUTIONS[resolution]
  return { width: { ideal: width, max: width }, height: { ideal: height, max: height }, frameRate: { ideal: fps, max: fps } }
}

export type Encoding = { maxBitrate: number; maxFramerate?: number }

/** 60 fps gets ~1.5x the bitrate of 30 fps at the same resolution. */
export const screenEncoding = ({ resolution, fps }: ScreenPreset): Encoding => ({
  maxBitrate: Math.round(RESOLUTIONS[resolution].mbps * (fps === 60 ? 1.5 : 1) * 1_000_000),
  maxFramerate: fps,
})

/** Text stays sharp at 30 fps ('detail' keeps resolution under pressure); 60 fps is for motion (keeps the frame rate). */
export const screenContentHint = ({ fps }: ScreenPreset) => (fps === 60 ? 'motion' : 'detail')

export const CAMERA_CONSTRAINTS: MediaTrackConstraints = { width: { ideal: 1280 }, height: { ideal: 720 }, frameRate: { ideal: 30 } }
export const CAMERA_ENCODING: Encoding = { maxBitrate: 2_500_000, maxFramerate: 30 }
export const SCREEN_AUDIO_ENCODING: Encoding = { maxBitrate: 128_000 }

type Codec = { mimeType: string; sdpFmtpLine?: string }

/** H.264 first: it has hardware encoders on most machines (the browser falls back to software by itself).
 * packetization-mode=1 before 0, and the other codecs (and RTX) stay as fallbacks. */
export const preferH264 = <C extends Codec>(codecs: readonly C[]): C[] => {
  const rank = (c: C) => {
    if (c.mimeType.toLowerCase() !== 'video/h264') return 2
    return /packetization-mode=1/.test(c.sdpFmtpLine ?? '') ? 0 : 1
  }
  return [...codecs].sort((a, b) => rank(a) - rank(b))
}
