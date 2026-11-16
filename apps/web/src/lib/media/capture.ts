// Getting media from the user's devices.
import workletUrl from '@/lib/noise-gate.worklet?worker&url'
import { CAMERA_CONSTRAINTS, screenConstraints, type ScreenPreset } from './presets'

export type Microphone = {
  /** What gets published (the noise gate's output when it is on). */
  track: MediaStreamTrack
  /** The device's own track: it ends when the device is unplugged (the noise gate's output never does). */
  device: MediaStreamTrack
  stop: () => void
}

/** Cuts the rumble below the voice (AC, desk thumps): constant and low, it would hold the gate open. */
const RUMBLE_HZ = 90

const device = (deviceId: string | null): MediaTrackConstraints => (deviceId ? { deviceId: { exact: deviceId } } : {})

const getUserMedia = async (constraints: MediaTrackConstraints, kind: 'audio' | 'video', deviceId: string | null) => {
  try {
    return await navigator.mediaDevices.getUserMedia({ [kind]: { ...constraints, ...device(deviceId) } })
  } catch (cause) {
    // The saved device is gone: fall back to the default one.
    if (deviceId && cause instanceof DOMException && cause.name === 'OverconstrainedError') {
      return navigator.mediaDevices.getUserMedia({ [kind]: constraints })
    }
    throw cause
  }
}

const withNoiseGate = async (raw: MediaStreamTrack): Promise<Microphone> => {
  const context = new AudioContext()
  try {
    await context.audioWorklet.addModule(workletUrl)
    const source = context.createMediaStreamSource(new MediaStream([raw]))
    const rumble = context.createBiquadFilter()
    rumble.type = 'highpass'
    rumble.frequency.value = RUMBLE_HZ
    const gate = new AudioWorkletNode(context, 'noise-gate')
    const destination = context.createMediaStreamDestination()
    source.connect(rumble).connect(gate).connect(destination)
    const [track] = destination.stream.getAudioTracks()
    if (!track) throw new Error('no output track')
    return {
      track,
      device: raw,
      stop: () => {
        track.stop()
        raw.stop()
        void context.close()
      },
    }
  } catch {
    void context.close()
    return { track: raw, device: raw, stop: () => raw.stop() } // the browser's own noise suppression still applies
  }
}

export const captureMicrophone = async (deviceId: string | null, noiseGate: boolean): Promise<Microphone> => {
  const stream = await getUserMedia(
    { echoCancellation: true, noiseSuppression: true, autoGainControl: true },
    'audio',
    deviceId,
  )
  const [raw] = stream.getAudioTracks()
  if (!raw) throw new DOMException('no audio track', 'NotFoundError')

  return noiseGate ? withNoiseGate(raw) : { track: raw, device: raw, stop: () => raw.stop() }
}

export const captureCamera = async (deviceId: string | null) => {
  const [track] = (await getUserMedia(CAMERA_CONSTRAINTS, 'video', deviceId)).getVideoTracks()
  if (!track) throw new DOMException('no video track', 'NotFoundError')

  return track
}

/** What the browser's picker opens on: a whole screen, a window or a tab (a hint; the user can still pick another). */
export type ScreenSurface = 'monitor' | 'window' | 'browser'

export type ScreenSource = { video: MediaStreamTrack; audio: MediaStreamTrack | null }

/** Screen video, plus its audio when asked for and the browser and the chosen source provide it. */
export const captureScreen = async (
  preset: ScreenPreset,
  { surface, audio }: { surface: ScreenSurface; audio: boolean },
): Promise<ScreenSource> => {
  const stream = await navigator.mediaDevices.getDisplayMedia({
    video: { ...screenConstraints(preset), displaySurface: surface },
    audio: audio && {
      echoCancellation: false,
      noiseSuppression: false,
      autoGainControl: false,
      restrictOwnAudio: true,
    },
    systemAudio: 'include',
    windowAudio: 'system',
    monitorTypeSurfaces: 'include',
    // Sharing this tab would show an infinite tunnel and loop the call's audio.
    selfBrowserSurface: 'exclude',
    surfaceSwitching: 'include',
  })
  const [video] = stream.getVideoTracks()
  if (!video) throw new DOMException('no video track', 'NotFoundError')

  return { video, audio: stream.getAudioTracks()[0] ?? null }
}

export type CaptureKind = 'microphone' | 'camera' | 'screen'

/** A user-facing explanation of a capture failure. */
export const captureError = (cause: unknown, kind: CaptureKind): string => {
  if (!window.isSecureContext) return 'Voz e vídeo exigem uma conexão segura (HTTPS ou localhost).'
  const name = cause instanceof DOMException || cause instanceof Error ? cause.name : ''
  const what = { microphone: 'o microfone', camera: 'a câmera', screen: 'a tela' }[kind]
  if (name === 'NotAllowedError') {
    return kind === 'screen'
      ? 'O compartilhamento de tela foi cancelado ou bloqueado.'
      : `A permissão para usar ${what} foi negada. Libere o acesso nas configurações do navegador.`
  }
  if (name === 'NotFoundError') return `Nenhum dispositivo encontrado para ${what}.`
  if (name === 'NotReadableError')
    return `Não foi possível abrir ${what}: o dispositivo pode estar em uso por outro aplicativo.`

  return `Não foi possível usar ${what}.`
}
