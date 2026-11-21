import { useEffect } from 'react'
import { useMotionValue, useSpring, type MotionValue } from 'motion/react'
import { blockLevel } from '@/lib/noise-gate'

const SPRING = { stiffness: 300, damping: 30, mass: 0.2 }

/** RMS below this is silence (~-40 dBFS). */
const SPEAKING_RMS = 0.01

let context: AudioContext | null = null

/**
 * 0..1 while the track carries speech. Goes straight into a MotionValue on every frame:
 * through React state, a dozen tiles at 60 Hz would re-render the stage constantly.
 */
export const useAudioLevel = (track: MediaStreamTrack | null | undefined): MotionValue<number> => {
  const raw = useMotionValue(0)
  const smoothed = useSpring(raw, SPRING)

  useEffect(() => {
    if (!track || track.kind !== 'audio') return
    context ??= new AudioContext()
    context.resume().catch(() => {})
    const source = context.createMediaStreamSource(new MediaStream([track]))
    const analyser = context.createAnalyser()
    analyser.fftSize = 512
    source.connect(analyser)
    const samples = new Float32Array(analyser.fftSize)
    let frame = 0
    const sample = () => {
      analyser.getFloatTimeDomainData(samples)
      const rms = blockLevel(samples)
      raw.set(rms > SPEAKING_RMS ? Math.min(1, 0.3 + rms * 5) : 0)
      frame = requestAnimationFrame(sample)
    }
    frame = requestAnimationFrame(sample)
    return () => {
      cancelAnimationFrame(frame)
      source.disconnect()
      raw.set(0)
    }
  }, [track, raw])

  return smoothed
}
