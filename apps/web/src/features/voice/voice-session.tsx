// The call the user is in (call.ts), for React: read it with useVoice(selector), act on it with `voice`.
import { useEffect, useRef, type ReactNode } from 'react'
import { useShallow } from 'zustand/react/shallow'
import { onGatewayEvent } from '@/lib/gateway'
import {
  dismissError,
  handleGatewayEvent,
  join,
  leave,
  setCamera,
  setDeafened,
  setDevice,
  setMuted,
  setNoiseGate,
  setQuality,
  setScreenAudio,
  setScreenPreset,
  setVolume,
  startScreen,
  stopScreen,
  switchScreen,
  useCall,
  watch,
  type CallState,
} from './call'

export { isMuted } from './call'

export const voice = {
  join,
  leave,
  setMuted,
  setDeafened,
  setCamera,
  startScreen,
  switchScreen,
  stopScreen,
  setScreenPreset,
  setScreenAudio,
  setDevice,
  setNoiseGate,
  watch,
  setQuality,
  setVolume,
  dismissError,
}

/** Part of the call state; a selector returning a new object is compared shallowly. */
export const useVoice = <T,>(pick: (s: CallState) => T): T => useCall(useShallow(pick))

type RemoteAudioProps = { track: MediaStreamTrack; muted: boolean; volume: number; sinkId: string | null }

const RemoteAudio = ({ track, muted, volume, sinkId }: RemoteAudioProps) => {
  const ref = useRef<HTMLAudioElement>(null)
  const gain = useRef<GainNode | null>(null)
  const boosted = volume > 1

  useEffect(() => {
    const el = ref.current
    if (!el) return
    el.srcObject = new MediaStream([track])
    el.play().catch(() => {}) // autoplay is allowed: joining was a click
    return () => {
      el.srcObject = null
    }
  }, [track])

  useEffect(() => {
    const el = ref.current
    if (el && sinkId && 'setSinkId' in el) el.setSinkId(sinkId).catch(() => {})
  }, [sinkId])

  // Above 100% the element cannot go, so a gain node plays it instead (the element keeps playing, muted:
  // Chrome only feeds a remote WebRTC track to WebAudio while a media element plays it).
  useEffect(() => {
    if (!boosted) return
    const context = new AudioContext()
    const node = context.createGain()
    context
      .createMediaStreamSource(new MediaStream([track]))
      .connect(node)
      .connect(context.destination)
    gain.current = node
    if (sinkId && 'setSinkId' in context)
      (context as AudioContext & { setSinkId: (id: string) => Promise<void> }).setSinkId(sinkId).catch(() => {})
    void context.resume()
    return () => {
      gain.current = null
      void context.close()
    }
  }, [track, boosted, sinkId])

  useEffect(() => {
    if (ref.current) ref.current.volume = Math.min(volume, 1)
    if (gain.current) gain.current.gain.value = muted ? 0 : volume
  }, [volume, muted, boosted, sinkId, track]) // the gain node is new whenever the effect above runs again

  return <audio ref={ref} autoPlay muted={muted || boosted} />
}

/** Everyone's audio, at the provider so it keeps playing wherever the user navigates. */
const CallAudio = () => {
  const { remote, deafened, output, volumes } = useVoice((s) => ({
    remote: s.remote,
    deafened: s.deafened,
    output: s.prefs.output,
    volumes: s.volumes,
  }))

  return (
    <div hidden>
      {remote
        .filter((r) => r.track.kind === 'audio')
        .map((r) => (
          <RemoteAudio
            key={`${r.userId}/${r.name}`}
            track={r.track}
            muted={deafened}
            volume={volumes[r.userId] ?? 1}
            sinkId={output}
          />
        ))}
    </div>
  )
}

/** Owns the call for the whole session: navigating never interrupts it. */
export const VoiceProvider = ({ children }: { children: ReactNode }) => {
  useEffect(() => {
    const off = onGatewayEvent(handleGatewayEvent)
    return () => {
      off()
      leave() // signed out
    }
  }, [])

  return (
    <>
      {children}
      <CallAudio />
    </>
  )
}
