// The call: one per app session, outside React so that navigating never touches it.
// The provider (voice-session.tsx) feeds it gateway events and exposes it through useVoice(selector).
import { create } from 'zustand'
import { captureCamera, captureError, captureMicrophone, type Microphone, type ScreenSource } from '@/lib/media/capture'
import {
  CAMERA_ENCODING,
  DEFAULT_SCREEN_PRESET,
  isScreenPreset,
  SCREEN_AUDIO_ENCODING,
  screenConstraints,
  screenContentHint,
  screenEncoding,
  type ScreenPreset,
} from '@/lib/media/presets'
import {
  createTransport,
  type Quality,
  type RemoteTrack,
  type TransportState,
  type VoiceTransport,
} from '@/lib/media/transport'
import { sendOp } from '@/lib/gateway'
import { ALL, has, Permission } from '@/lib/permissions'
import { getState, type State } from '@/lib/store'
import type { GatewayEvent, GatewayEvents, TrackName } from '@/lib/wire'

export type Connection = 'idle' | 'connecting' | 'connected' | 'failed'

export type Prefs = {
  input: string | null
  output: string | null
  camera: string | null
  noiseGate: boolean
  screen: ScreenPreset
  /** Ask for the source's audio when picking a screen to share. */
  screenAudio: boolean
}

export type CallState = {
  channelId: string | null
  connection: Connection
  error: string | null
  /** Our own mute choice; deafened mutes on top of it. */
  selfMuted: boolean
  deafened: boolean
  /** No microphone published: no Speak permission, or it could not be opened. */
  listenOnly: boolean
  mic: MediaStreamTrack | null
  camera: MediaStreamTrack | null
  screen: MediaStreamTrack | null
  remote: RemoteTrack[]
  /** `${userId}/${track}` -> watching that video. */
  watching: Record<string, boolean>
  /** `${userId}/${track}` -> the layer asked for (auto when absent). */
  qualities: Record<string, Quality>
  /** User ID -> how loud we play them, 0 to 2 (1 when absent). Kept across sessions. */
  volumes: Record<string, number>
  prefs: Prefs
}

const CONNECT_TIMEOUT_MS = 15_000

const UDP_BLOCKED = 'Não conectou: a porta UDP de mídia do servidor pode estar bloqueada.'

const PREFS_KEY = 'discordo:voice-prefs'

const VOLUMES_KEY = 'discordo:voice-volumes'

const MIC_LOST = 'Seu microfone foi desconectado. Conecte um microfone para voltar a falar.'

const loadPrefs = (): Prefs => {
  const prefs: Prefs = {
    input: null,
    output: null,
    camera: null,
    noiseGate: true,
    screen: DEFAULT_SCREEN_PRESET,
    screenAudio: true,
  }

  try {
    const saved: unknown = JSON.parse(localStorage.getItem(PREFS_KEY) ?? '{}')
    if (typeof saved !== 'object' || saved === null) return prefs
    const s = saved as Record<string, unknown>
    for (const k of ['input', 'output', 'camera'] as const) if (typeof s[k] === 'string') prefs[k] = s[k]
    if (typeof s.noiseGate === 'boolean') prefs.noiseGate = s.noiseGate
    if (typeof s.screenAudio === 'boolean') prefs.screenAudio = s.screenAudio
    if (isScreenPreset(s.screen)) prefs.screen = s.screen
  } catch {
    // Unavailable or corrupt storage: defaults.
  }

  return prefs
}

export const clampVolume = (v: number) => (Number.isFinite(v) ? Math.min(2, Math.max(0, v)) : 1)

const loadVolumes = (): Record<string, number> => {
  try {
    const saved: unknown = JSON.parse(localStorage.getItem(VOLUMES_KEY) ?? '{}')
    if (typeof saved !== 'object' || saved === null) return {}
    return Object.fromEntries(
      Object.entries(saved).flatMap(([id, v]) => (typeof v === 'number' ? [[id, clampVolume(v)]] : [])),
    )
  } catch {
    return {}
  }
}

const IDLE = {
  connection: 'idle',
  error: null,
  listenOnly: false,
  mic: null,
  camera: null,
  screen: null,
  remote: [],
  watching: {},
  qualities: {},
} as const satisfies Partial<CallState>

export const useCall = create<CallState>()(() => ({
  ...IDLE,
  channelId: null,
  selfMuted: false,
  deafened: false,
  volumes: loadVolumes(),
  prefs: loadPrefs(),
}))

const set = (patch: Partial<CallState>) => useCall.setState(patch)

const call = useCall.getState

/** Our permission bits in a call's channel (a number, so it is safe in a store selector). DMs allow everything. */
export const callPermissions = (s: State, channelId: string) => {
  for (const guild of s.guilds) {
    const channel = guild.channels.find((c) => c.id === channelId)
    if (channel) return channel.permissions
  }

  return ALL
}

// ---- private session state ----

let transport: VoiceTransport | null = null

let microphone: Microphone | null = null

let screenAudio: MediaStreamTrack | null = null

/** Join sent, VOICE_READY not received yet. */
let pendingJoin: string | null = null

/** Bumped whenever the media session is torn down, so late async work knows it is stale. */
let generation = 0

let timer: ReturnType<typeof setTimeout> | undefined

let mediaState: TransportState = 'connecting'

let micDone = false

let everConnected = false

/** The microphone was unplugged and no other one could be opened yet. */
let micLost = false

/** Effective mute: our own choice, deafened, or listen-only. */
export const isMuted = (s: CallState) => s.selfMuted || s.deafened || s.listenOnly

const muted = () => isMuted(call())

const sendVoiceState = () => void sendOp('voice_state', { muted: muted(), deafened: call().deafened })

const stopMedia = () => {
  generation++
  clearTimeout(timer)
  transport?.close()
  transport = null
  microphone?.stop()
  microphone = null
  call().camera?.stop()
  call().screen?.stop()
  screenAudio?.stop()
  screenAudio = null
  micDone = false
  micLost = false
  navigator.mediaDevices?.removeEventListener('devicechange', onDeviceChange)
}

const fail = (error: string) => {
  stopMedia()
  pendingJoin = null
  if (call().channelId) sendOp('voice_leave')
  set({ ...IDLE, connection: 'failed', error })
}

const start = (channelId: string) => {
  stopMedia()
  pendingJoin = channelId
  everConnected = false
  set({ ...IDLE, channelId, connection: 'connecting' })
  if (!sendOp('voice_join', { channel_id: channelId })) return fail('Sem conexão com o servidor.')
  timer = setTimeout(() => {
    if (call().connection === 'connecting')
      fail(pendingJoin ? 'O servidor não respondeu ao pedido de entrar na chamada.' : UDP_BLOCKED)
  }, CONNECT_TIMEOUT_MS)
}

export const join = (channelId: string) => {
  if (call().channelId === channelId && call().connection !== 'failed') return
  start(channelId)
}

export const leave = () => {
  if (call().channelId && call().connection !== 'failed') sendOp('voice_leave')
  stopMedia()
  pendingJoin = null
  set({ ...IDLE, channelId: null })
}

/** Reflects the transport's state once our microphone is set up (so we never flash "connected" before publishing). */
const sync = () => {
  if (!micDone || call().connection === 'idle' || call().connection === 'failed') return
  if (mediaState === 'failed') return fail(everConnected ? 'A conexão de mídia caiu.' : UDP_BLOCKED)
  if (mediaState === 'connected') {
    everConnected = true
    clearTimeout(timer)
  }
  set({ connection: mediaState })
}

const startMicrophone = async (gen: number, t: VoiceTransport, channelId: string) => {
  let listenOnly = !has(callPermissions(getState(), channelId), Permission.Speak)
  let error: string | null = null
  if (!listenOnly) {
    try {
      const mic = await captureMicrophone(call().prefs.input, call().prefs.noiseGate)
      if (gen !== generation) return mic.stop()
      mic.track.enabled = !muted()
      microphone = mic
      watchMicrophone(mic)
      set({ mic: mic.track })
      void t.publish('mic', mic.track) // creates the publish connection synchronously; negotiation continues in the background
    } catch (cause) {
      listenOnly = true
      error = `${captureError(cause, 'microphone')} Você está só ouvindo.`
    }
  }
  if (gen !== generation) return
  set({ listenOnly, error })
  sendVoiceState()
  micDone = true
  sync()
}

const onVoiceReady = (d: GatewayEvents['VOICE_READY']) => {
  if (d.channel_id !== call().channelId) return // a newer join (or a leave) is on its way
  pendingJoin = null
  transport?.close()
  const gen = ++generation
  mediaState = 'connecting'
  const t = createTransport(d.connection, sendOp, {
    onRemoteTracks: (remote) => {
      if (gen === generation) set({ remote })
    },
    onState: (s) => {
      if (gen !== generation) return
      mediaState = s
      sync()
    },
    onError: (error) => {
      if (gen === generation) set({ error })
    },
  })
  if (!t) return fail(`Este app não suporta o tipo de conexão de mídia do servidor ("${d.connection.kind}").`)
  transport = t
  navigator.mediaDevices?.addEventListener('devicechange', onDeviceChange)
  void startMicrophone(gen, t, d.channel_id)
}

const ERRORS: Record<string, string> = {
  forbidden: 'Você não tem permissão para entrar nesta chamada.',
  'not found': 'Esta chamada não existe mais.',
}

const onVoiceError = ({ op, error }: GatewayEvents['VOICE_ERROR']) => {
  const text = ERRORS[error] ?? error
  if (op === 'voice_join' || op === 'rtc_offer' || op === 'rtc_answer') {
    if (call().channelId) fail(`Erro na chamada: ${text}`)
  } else if (op !== 'voice_state' && call().channelId) {
    set({ error: `Erro na chamada: ${text}` })
  }
}

export const handleGatewayEvent = (e: GatewayEvent) => {
  switch (e.t) {
    case 'READY': {
      // A new gateway connection: the server already ended the call that belonged to the old one.
      const { channelId } = call()
      if (channelId) start(channelId)
      return
    }
    case 'VOICE_READY':
      return onVoiceReady(e.d)
    case 'VOICE_ERROR':
      return onVoiceError(e.d)
    case 'RTC_OFFER':
    case 'RTC_ANSWER':
      return transport?.handle(e)
    case 'VOICE_STATE_UPDATE': {
      // We are now in another call without asking for it here (another tab joined one).
      const moved = e.d.user_id === getState().me?.id && e.d.channel_id && e.d.channel_id !== call().channelId
      if (moved && call().channelId && !pendingJoin && call().connection !== 'failed') {
        stopMedia()
        set({ ...IDLE, channelId: null, error: 'Você entrou em outra chamada em outra janela.' })
      }
      return
    }
  }
}

// ---- controls ----

const applyMute = () => {
  if (microphone) microphone.track.enabled = !muted()
  if (transport) sendVoiceState()
}

export const setMuted = (selfMuted: boolean) => {
  set(selfMuted ? { selfMuted } : { selfMuted, deafened: false })
  applyMute()
}

export const setDeafened = (deafened: boolean) => {
  set({ deafened })
  applyMute()
}

export const setCamera = async (on: boolean) => {
  const t = transport
  if (!t) return
  if (!on) {
    const track = call().camera
    set({ camera: null })
    track?.stop()
    return t.publish('camera', null)
  }
  if (call().camera) return
  const gen = generation
  try {
    const track = await captureCamera(call().prefs.camera)
    if (gen !== generation) return track.stop()
    track.addEventListener('ended', () => {
      if (call().camera === track) void setCamera(false)
    })
    set({ camera: track, error: null })
    await t.publish('camera', track, CAMERA_ENCODING)
  } catch (cause) {
    set({ error: captureError(cause, 'camera') })
  }
}

export const stopScreen = async () => {
  const t = transport
  const video = call().screen
  const audio = screenAudio
  screenAudio = null
  set({ screen: null })
  video?.stop()
  audio?.stop()
  if (!t) return
  await t.publish('screen', null)
  await t.publish('screen-audio', null)
}

/** Shares a source picked in the share dialog. The call takes the tracks over (and stops them when done). */
export const startScreen = async (source: ScreenSource) => {
  const t = transport
  const { video, audio } = source

  if (!t || call().screen) {
    video.stop()
    audio?.stop()
    return
  }

  const preset = call().prefs.screen
  video.contentHint = screenContentHint(preset)

  // The browser's own "stop sharing" button ends the track.
  video.addEventListener('ended', () => {
    if (call().screen === video) void stopScreen()
  })

  screenAudio = audio
  set({ screen: video, error: null })

  try {
    await t.publish('screen', video, screenEncoding(preset))
    if (audio) await t.publish('screen-audio', audio, SCREEN_AUDIO_ENCODING)
  } catch (cause) {
    set({ error: captureError(cause, 'screen') })
  }
}

/** Shares another source in place of the current one, on the same senders (no renegotiation, same encodings). */
export const switchScreen = async (source: ScreenSource) => {
  const t = transport
  const old = call().screen
  const { video, audio } = source

  if (!t || !old) {
    video.stop()
    audio?.stop()
    return
  }

  video.contentHint = screenContentHint(call().prefs.screen)
  video.addEventListener('ended', () => {
    if (call().screen === video) void stopScreen()
  })

  const oldAudio = screenAudio
  screenAudio = audio
  set({ screen: video, error: null })

  try {
    await t.publish('screen', video, screenEncoding(call().prefs.screen))

    // The new source may bring audio where the old one had none, or the other way around.
    if (audio) await t.publish('screen-audio', audio, SCREEN_AUDIO_ENCODING)
    else if (oldAudio) await t.publish('screen-audio', null)
  } catch (cause) {
    set({ error: captureError(cause, 'screen') })
  }

  old.stop() // after the swap: its "ended" no longer stops sharing, since it is not call().screen
  oldAudio?.stop()
}

const savePrefs = (patch: Partial<Prefs>) => {
  set({ prefs: { ...call().prefs, ...patch } })
  try {
    localStorage.setItem(PREFS_KEY, JSON.stringify(call().prefs))
  } catch {
    // Still applies for this session.
  }
}

export const setScreenAudio = (screenAudio: boolean) => savePrefs({ screenAudio })

export const setScreenPreset = async (screen: ScreenPreset) => {
  savePrefs({ screen })
  const video = call().screen
  if (!video || !transport) return
  try {
    await video.applyConstraints(screenConstraints(screen))
    video.contentHint = screenContentHint(screen)
    await transport.publish('screen', video, screenEncoding(screen))
  } catch {
    set({ error: 'Não foi possível mudar a qualidade da tela compartilhada.' })
  }
}

/** Opens the microphone again (another device, the noise gate toggled, or the device unplugged) and swaps
 * it into the sender: replaceTrack, no renegotiation. */
const restartMicrophone = async () => {
  const t = transport
  const old = microphone
  if (!t || (!old && !micLost)) return
  const gen = generation
  try {
    const mic = await captureMicrophone(call().prefs.input, call().prefs.noiseGate)
    if (gen !== generation || microphone !== old) return mic.stop()
    mic.track.enabled = !muted()
    await t.publish('mic', mic.track)
    microphone = mic
    micLost = false
    watchMicrophone(mic)
    old?.stop()
    set({ mic: mic.track, ...(call().error === MIC_LOST ? { error: null } : {}) })
  } catch (cause) {
    set({ error: micLost ? MIC_LOST : captureError(cause, 'microphone') })
  }
}

/** An unplugged device ends its track: fall back to the default one (captureMicrophone does), or wait for one. */
const watchMicrophone = (mic: Microphone) => {
  mic.device.addEventListener('ended', () => {
    if (microphone !== mic) return
    micLost = true
    microphone = null
    mic.stop()
    set({ mic: null })
    void restartMicrophone()
  })
}

/** A device plugged in: the microphone we lost, or the chosen one back after a fallback to the default. */
const onDeviceChange = async () => {
  if (!transport) return
  if (micLost) return restartMicrophone()
  const chosen = call().prefs.input
  if (!chosen || !microphone || microphone.device.getSettings().deviceId === chosen) return
  const devices = await navigator.mediaDevices.enumerateDevices().catch(() => [])
  if (devices.some((d) => d.kind === 'audioinput' && d.deviceId === chosen)) await restartMicrophone()
}

export const setNoiseGate = (noiseGate: boolean) => {
  savePrefs({ noiseGate })
  void restartMicrophone()
}

export const setDevice = async (kind: 'input' | 'output' | 'camera', deviceId: string) => {
  savePrefs({ [kind]: deviceId })
  if (kind === 'input') return restartMicrophone()
  const old = call().camera
  if (kind !== 'camera' || !old || !transport) return
  const t = transport
  try {
    const track = await captureCamera(deviceId)
    if (call().camera !== old) return track.stop()
    track.addEventListener('ended', () => {
      if (call().camera === track) void setCamera(false)
    })
    await t.publish('camera', track, CAMERA_ENCODING)
    old.stop()
    set({ camera: track })
  } catch (cause) {
    set({ error: captureError(cause, 'camera') })
  }
}

export const watchKey = (userId: string, track: 'camera' | 'screen') => `${userId}/${track}`

export const watch = (userId: string, track: 'camera' | 'screen', on: boolean) => {
  if (!transport) return
  const key = watchKey(userId, track)
  set({ watching: { ...call().watching, [key]: on } })
  transport.subscribe(userId, track, on, call().qualities[key] ?? 'auto')
}

export const setQuality = (userId: string, track: 'camera' | 'screen', quality: Quality) => {
  const key = watchKey(userId, track)
  set({ qualities: { ...call().qualities, [key]: quality } })
  if (transport && call().watching[key]) transport.subscribe(userId, track, true, quality)
}

export const setVolume = (userId: string, volume: number) => {
  set({ volumes: { ...call().volumes, [userId]: clampVolume(volume) } })
  try {
    localStorage.setItem(VOLUMES_KEY, JSON.stringify(call().volumes))
  } catch {
    // Still applies for this session.
  }
}

/** Sender stats of one of our published videos, for the encoder indicator. */
export const senderStats = (name: TrackName) => transport?.getStats?.(name) ?? Promise.resolve(null)

export const dismissError = () => set({ error: null })
