import { Track } from 'livekit-client'
import { describe, expect, it } from 'vitest'
import type { TrackName } from '@/lib/wire'
import { nameOf, sourceOf } from './livekit'

describe('LiveKit track sources', () => {
  it('maps every track name to its source and back', () => {
    const names: TrackName[] = ['mic', 'camera', 'screen', 'screen-audio']
    expect(names.map(sourceOf)).toEqual([
      Track.Source.Microphone,
      Track.Source.Camera,
      Track.Source.ScreenShare,
      Track.Source.ScreenShareAudio,
    ])
    for (const name of names) expect(nameOf(sourceOf(name))).toBe(name)
  })

  it('ignores sources we do not publish', () => {
    expect(nameOf(Track.Source.Unknown)).toBeUndefined()
  })
})
