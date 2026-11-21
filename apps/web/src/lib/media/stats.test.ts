import { describe, expect, it } from 'vitest'
import { encoderDetails, encoderLabel, encoderStats } from './stats'

const report = (entries: Record<string, unknown>[]) =>
  new Map(entries.map((e, i) => [(e.id as string | undefined) ?? String(i), e])) as unknown as RTCStatsReport

describe('encoder stats', () => {
  it('sums simulcast layers and reports the largest one', () => {
    const layer = {
      type: 'outbound-rtp',
      encoderImplementation: 'SimulcastEncoderAdapter (ExternalEncoder)',
      powerEfficientEncoder: true,
    }
    const first = encoderStats(
      report([
        { type: 'codec', id: 'c1', mimeType: 'video/H264', timestamp: 1000 },
        {
          ...layer,
          codecId: 'c1',
          timestamp: 1000,
          bytesSent: 1000,
          frameWidth: 640,
          frameHeight: 360,
          framesPerSecond: 30,
        },
        { ...layer, timestamp: 1000, bytesSent: 4000, frameWidth: 1920, frameHeight: 1080, framesPerSecond: 29.6 },
        { type: 'media-source', timestamp: 1000 },
      ]),
      null,
    )
    expect(first).toMatchObject({
      width: 1920,
      height: 1080,
      fps: 29.6,
      bitrate: null,
      bytesSent: 5000,
      hardware: true,
    })
    const second = encoderStats(
      report([
        { ...layer, timestamp: 3000, bytesSent: 251_000 },
        { ...layer, timestamp: 3000, bytesSent: 254_000, frameWidth: 1920, frameHeight: 1080, framesPerSecond: 30 },
      ]),
      first,
    )
    expect(second.bitrate).toBe(2_000_000)
    expect(first.codec).toBe('H264')
    expect(encoderLabel({ ...second, codec: first.codec })).toBe(
      'Codificação: GPU (H264, SimulcastEncoderAdapter (ExternalEncoder))',
    )
    expect(encoderDetails(second)).toBe('1920×1080 · 30 fps · 2,0 Mbps')
  })

  it('says CPU or unknown', () => {
    const cpu = encoderStats(
      report([{ type: 'outbound-rtp', timestamp: 1, encoderImplementation: 'libvpx', powerEfficientEncoder: false }]),
      null,
    )
    expect(encoderLabel(cpu)).toBe('Codificação: CPU (libvpx)')
    const unknown = encoderStats(report([]), null)
    expect(encoderLabel(unknown)).toBe('Codificação: desconhecida')
    expect(encoderDetails(unknown)).toBe('Sem dados ainda')
  })
})

describe('limit', () => {
  it('says what the browser cuts quality for', () => {
    const report = new Map([
      [
        'a',
        { type: 'outbound-rtp', timestamp: 1, frameWidth: 1920, frameHeight: 1080, qualityLimitationReason: 'cpu' },
      ],
    ]) as unknown as RTCStatsReport
    const s = encoderStats(report, null)
    expect(s.limit).toBe('cpu')
    expect(encoderDetails(s)).toContain('limitada pela CPU')
  })
})
