import { describe, expect, it } from 'vitest'
import { orderCodecs } from './codecs'

const codecs = [
  { mimeType: 'video/VP8' },
  { mimeType: 'video/rtx', sdpFmtpLine: 'apt=96' },
  { mimeType: 'video/H264', sdpFmtpLine: 'packetization-mode=0;profile-level-id=42e01f' },
  { mimeType: 'video/H264', sdpFmtpLine: 'packetization-mode=1;profile-level-id=42e01f' },
  { mimeType: 'video/AV1' },
  { mimeType: 'video/VP9', sdpFmtpLine: 'profile-id=0' },
]
const order = (hw: string[]) => orderCodecs(codecs, new Set(hw)).map((c) => `${c.mimeType} ${c.sdpFmtpLine ?? ''}`.trim())

describe('orderCodecs', () => {
  it('puts GPU codecs first, then the CPU ones, RTX last', () => {
    expect(order(['video/av1', 'video/vp9'])).toEqual([
      'video/VP9 profile-id=0',
      'video/AV1',
      'video/H264 packetization-mode=1;profile-level-id=42e01f',
      'video/H264 packetization-mode=0;profile-level-id=42e01f',
      'video/VP8',
      'video/rtx apt=96',
    ])
  })

  it('puts the chosen codec first, even over the GPU ones', () => {
    expect(orderCodecs(codecs, new Set(['video/h264']), 'av1')[0]?.mimeType).toBe('video/AV1')
  })

  it('prefers H.264 when nothing is on the GPU', () => {
    expect(order([])[0]).toBe('video/H264 packetization-mode=1;profile-level-id=42e01f')
  })
})
