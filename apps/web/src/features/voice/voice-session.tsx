// CONTRACT (implemented by the voice area): the call the user is in, and how to join or leave one.
import { useEffect, useMemo, useRef, useSyncExternalStore, type ReactNode } from 'react'
import { onGatewayEvent } from '@/lib/gateway'
import {
  dismissError, getCall, handleGatewayEvent, join, leave, setCamera, setDeafened, setDevice, setMuted, setNoiseGate,
  setScreenPreset, startScreen, stopScreen, subscribeCall, watch, type CallState,
} from './call'

const actions = { join, leave, setMuted, setDeafened, setCamera, startScreen, stopScreen, setScreenPreset, setDevice, setNoiseGate, watch, dismissError }

export type VoiceApi = CallState & typeof actions & {
  /** Effective: our own mute, deafened, or listen-only. */
  muted: boolean
}

export const useVoice = (): VoiceApi => {
  const call = useSyncExternalStore(subscribeCall, getCall)
  return useMemo(() => ({ ...call, ...actions, muted: call.selfMuted || call.deafened || call.listenOnly }), [call])
}

const RemoteAudio = ({ track, muted, sinkId }: { track: MediaStreamTrack; muted: boolean; sinkId: string | null }) => {
  const ref = useRef<HTMLAudioElement>(null)

  useEffect(() => {
    const el = ref.current
    if (!el) return
    el.srcObject = new MediaStream([track])
    el.play().catch(() => {}) // autoplay is allowed: joining was a click
    return () => { el.srcObject = null }
  }, [track])

  useEffect(() => {
    const el = ref.current
    if (el && sinkId && 'setSinkId' in el) el.setSinkId(sinkId).catch(() => {})
  }, [sinkId])

  return <audio ref={ref} autoPlay muted={muted} />
}

/** Everyone's audio, at the provider so it keeps playing wherever the user navigates. */
const CallAudio = () => {
  const { remote, deafened, prefs } = useVoice()
  return (
    <div hidden>
      {remote.filter((r) => r.track.kind === 'audio').map((r) => (
        <RemoteAudio key={`${r.userId}/${r.name}`} track={r.track} muted={deafened} sinkId={prefs.output} />
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
