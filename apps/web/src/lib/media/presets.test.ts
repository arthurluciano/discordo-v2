import { describe, expect, it } from 'vitest'
import { isScreenPreset, preferH264, screenConstraints, screenContentHint, screenEncoding } from './presets'

describe('screen presets', () => {
  it('caps capture at the preset', () => {
    expect(screenConstraints({ resolution: '2160p', fps: 60 })).toEqual({
      width: { ideal: 3840, max: 3840 }, height: { ideal: 2160, max: 2160 }, frameRate: { ideal: 60, max: 60 },
    })
  })

  it('scales bitrate with resolution and frame rate', () => {
    expect(screenEncoding({ resolution: '480p', fps: 30 })).toEqual({ maxBitrate: 1_500_000, maxFramerate: 30 })
    expect(screenEncoding({ resolution: '1080p', fps: 30 }).maxBitrate).toBe(6_000_000)
    expect(screenEncoding({ resolution: '1080p', fps: 60 })).toEqual({ maxBitrate: 9_000_000, maxFramerate: 60 })
    expect(screenEncoding({ resolution: '2160p', fps: 60 }).maxBitrate).toBe(30_000_000)
  })

  it('hints detail at 30 fps and motion at 60', () => {
    expect(screenContentHint({ resolution: '720p', fps: 30 })).toBe('detail')
    expect(screenContentHint({ resolution: '720p', fps: 60 })).toBe('motion')
  })

  it('validates stored presets', () => {
    expect(isScreenPreset({ resolution: '1440p', fps: 60 })).toBe(true)
    expect(isScreenPreset({ resolution: '8k', fps: 60 })).toBe(false)
    expect(isScreenPreset({ resolution: '720p', fps: 24 })).toBe(false)
    expect(isScreenPreset(null)).toBe(false)
  })
})

describe('preferH264', () => {
  it('puts H.264 (packetization-mode=1 first) ahead of the rest, keeping their order', () => {
    const codecs = [
      { mimeType: 'video/VP8' },
      { mimeType: 'video/rtx', sdpFmtpLine: 'apt=96' },
      { mimeType: 'video/H264', sdpFmtpLine: 'level-asymmetry-allowed=1;packetization-mode=0;profile-level-id=42e01f' },
      { mimeType: 'video/H264', sdpFmtpLine: 'level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f' },
      { mimeType: 'video/AV1' },
    ]
    expect(preferH264(codecs).map((c) => `${c.mimeType} ${c.sdpFmtpLine ?? ''}`.trim())).toEqual([
      'video/H264 level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f',
      'video/H264 level-asymmetry-allowed=1;packetization-mode=0;profile-level-id=42e01f',
      'video/VP8',
      'video/rtx apt=96',
      'video/AV1',
    ])
  })
})
