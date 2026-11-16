// The call: one per app session, outside React so that navigating never touches it.
// The provider (voice-session.tsx) feeds it gateway events and exposes it through useVoice().
import { captureCamera, captureError, captureMicrophone, captureScreen, type Microphone } from '@/lib/media/capture'
import {
  CAMERA_ENCODING, DEFAULT_SCREEN_PRESET, isScreenPreset, SCREEN_AUDIO_ENCODING, screenConstraints, screenContentHint,
  screenEncoding, type ScreenPreset,
} from '@/lib/media/presets'
import { createTransport, type RemoteTrack, type TransportState, type VoiceTransport } from '@/lib/media/transport'
import { sendOp } from '@/lib/gateway'
import { ALL, channelPermissions, has, Permission } from '@/lib/permissions'
import { getState, type State } from '@/lib/store'
import type { GatewayEvent, GatewayEvents } from '@/lib/wire'

export type Connection = 'idle' | 'connecting' | 'connected' | 'failed'

export type Prefs = {
  input: string | null
  output: string | null
  camera: string | null
  noiseGate: boolean
  screen: ScreenPreset
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
  prefs: Prefs
}

const CONNECT_TIMEOUT_MS = 15_000
const UDP_BLOCKED = 'Não conectou: a porta UDP de mídia do servidor pode estar bloqueada.'
const PREFS_KEY = 'discordo:voice-prefs'

const loadPrefs = (): Prefs => {
  const prefs: Prefs = { input: null, output: null, camera: null, noiseGate: true, screen: DEFAULT_SCREEN_PRESET }
  try {
    const saved: unknown = JSON.parse(localStorage.getItem(PREFS_KEY) ?? '{}')
    if (typeof saved !== 'object' || saved === null) return prefs
    const s = saved as Record<string, unknown>
    for (const k of ['input', 'output', 'camera'] as const) if (typeof s[k] === 'string') prefs[k] = s[k]
    if (typeof s.noiseGate === 'boolean') prefs.noiseGate = s.noiseGate
    if (isScreenPreset(s.screen)) prefs.screen = s.screen
  } catch {
    // Unavailable or corrupt storage: defaults.
  }
  return prefs
}

const IDLE = {
  connection: 'idle', error: null, listenOnly: false, mic: null, camera: null, screen: null, remote: [], watching: {},
} as const satisfies Partial<CallState>

let state: CallState = { ...IDLE, channelId: null, selfMuted: false, deafened: false, prefs: loadPrefs() }
const listeners = new Set<() => void>()

const set = (patch: Partial<CallState>) => {
  state = { ...state, ...patch }
  listeners.forEach((l) => l())
}

export const getCall = () => state
export const subscribeCall = (listener: () => void) => {
  listeners.add(listener)
  return () => void listeners.delete(listener)
}

/** Our permission bits in a call's channel (a number, so it is safe in a store selector). DMs allow everything. */
export const callPermissions = (s: State, channelId: string) => {
  for (const guild of s.guilds) {
    const channel = guild.channels.find((c) => c.id === channelId)
    if (channel) return s.me ? channelPermissions(guild, channel, s.me.id) : 0
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

const muted = () => state.selfMuted || state.deafened || state.listenOnly

const sendVoiceState = () => void sendOp('voice_state', { muted: muted(), deafened: state.deafened })

const stopMedia = () => {
  generation++
  clearTimeout(timer)
  transport?.close()
  transport = null
  microphone?.stop()
  microphone = null
  state.camera?.stop()
  state.screen?.stop()
  screenAudio?.stop()
  screenAudio = null
  micDone = false
}

const fail = (error: string) => {
  stopMedia()
  pendingJoin = null
  if (state.channelId) sendOp('voice_leave')
  set({ ...IDLE, connection: 'failed', error })
}

const start = (channelId: string) => {
  stopMedia()
  pendingJoin = channelId
  everConnected = false
  set({ ...IDLE, channelId, connection: 'connecting' })
  if (!sendOp('voice_join', { channel_id: channelId })) return fail('Sem conexão com o servidor.')
  timer = setTimeout(() => {
    if (state.connection === 'connecting') fail(pendingJoin ? 'O servidor não respondeu ao pedido de entrar na chamada.' : UDP_BLOCKED)
  }, CONNECT_TIMEOUT_MS)
}

export const join = (channelId: string) => {
  if (state.channelId === channelId && state.connection !== 'failed') return
  start(channelId)
}

export const leave = () => {
  if (state.channelId && state.connection !== 'failed') sendOp('voice_leave')
  stopMedia()
  pendingJoin = null
  set({ ...IDLE, channelId: null })
}

/** Reflects the transport's state once our microphone is set up (so we never flash "connected" before publishing). */
const sync = () => {
  if (!micDone || state.connection === 'idle' || state.connection === 'failed') return
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
      const mic = await captureMicrophone(state.prefs.input, state.prefs.noiseGate)
      if (gen !== generation) return mic.stop()
      mic.track.enabled = !muted()
      microphone = mic
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
  if (d.channel_id !== state.channelId) return // a newer join (or a leave) is on its way
  pendingJoin = null
  transport?.close()
  const gen = ++generation
  mediaState = 'connecting'
  const t = createTransport(d.connection.kind, sendOp, {
    onRemoteTracks: (remote) => { if (gen === generation) set({ remote }) },
    onState: (s) => {
      if (gen !== generation) return
      mediaState = s
      sync()
    },
    onError: (error) => { if (gen === generation) set({ error }) },
  })
  if (!t) return fail(`Este app não suporta o tipo de conexão de mídia do servidor ("${d.connection.kind}").`)
  transport = t
  void startMicrophone(gen, t, d.channel_id)
}

const ERRORS: Record<string, string> = {
  forbidden: 'Você não tem permissão para entrar nesta chamada.',
  'not found': 'Esta chamada não existe mais.',
}

const onVoiceError = ({ op, error }: GatewayEvents['VOICE_ERROR']) => {
  const text = ERRORS[error] ?? error
  if (op === 'voice_join' || op === 'rtc_offer' || op === 'rtc_answer') {
    if (state.channelId) fail(`Erro na chamada: ${text}`)
  } else if (op !== 'voice_state' && state.channelId) {
    set({ error: `Erro na chamada: ${text}` })
  }
}

export const handleGatewayEvent = (e: GatewayEvent) => {
  switch (e.t) {
    case 'READY':
      // A new gateway connection: the server already ended the call that belonged to the old one.
      if (state.channelId) start(state.channelId)
      return
    case 'VOICE_READY':
      return onVoiceReady(e.d)
    case 'VOICE_ERROR':
      return onVoiceError(e.d)
    case 'RTC_OFFER':
    case 'RTC_ANSWER':
      return transport?.handle(e)
    case 'VOICE_STATE_UPDATE': {
      // We are now in another call without asking for it here (another tab joined one).
      const moved = e.d.user_id === getState().me?.id && e.d.channel_id && e.d.channel_id !== state.channelId
      if (moved && state.channelId && !pendingJoin && state.connection !== 'failed') {
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
    const track = state.camera
    set({ camera: null })
    track?.stop()
    return t.publish('camera', null)
  }
  if (state.camera) return
  const gen = generation
  try {
    const track = await captureCamera(state.prefs.camera)
    if (gen !== generation) return track.stop()
    track.addEventListener('ended', () => { if (state.camera === track) void setCamera(false) })
    set({ camera: track, error: null })
    await t.publish('camera', track, CAMERA_ENCODING)
  } catch (cause) {
    set({ error: captureError(cause, 'camera') })
  }
}

export const stopScreen = async () => {
  const t = transport
  const video = state.screen
  const audio = screenAudio
  screenAudio = null
  set({ screen: null })
  video?.stop()
  audio?.stop()
  if (!t) return
  await t.publish('screen', null)
  await t.publish('screen-audio', null)
}

export const startScreen = async () => {
  const t = transport
  if (!t || state.screen) return
  const gen = generation
  const preset = state.prefs.screen
  try {
    const { video, audio } = await captureScreen(preset)
    if (gen !== generation) {
      video.stop()
      audio?.stop()
      return
    }
    video.contentHint = screenContentHint(preset)
    // The browser's own "stop sharing" button ends the track.
    video.addEventListener('ended', () => { if (state.screen === video) void stopScreen() })
    screenAudio = audio
    set({ screen: video, error: null })
    await t.publish('screen', video, screenEncoding(preset))
    if (audio) await t.publish('screen-audio', audio, SCREEN_AUDIO_ENCODING)
  } catch (cause) {
    set({ error: captureError(cause, 'screen') })
  }
}

const savePrefs = (patch: Partial<Prefs>) => {
  set({ prefs: { ...state.prefs, ...patch } })
  try {
    localStorage.setItem(PREFS_KEY, JSON.stringify(state.prefs))
  } catch {
    // Still applies for this session.
  }
}

export const setScreenPreset = async (screen: ScreenPreset) => {
  savePrefs({ screen })
  const video = state.screen
  if (!video || !transport) return
  try {
    await video.applyConstraints(screenConstraints(screen))
    video.contentHint = screenContentHint(screen)
    await transport.publish('screen', video, screenEncoding(screen))
  } catch {
    set({ error: 'Não foi possível mudar a qualidade da tela compartilhada.' })
  }
}

const restartMicrophone = async () => {
  const t = transport
  const old = microphone
  if (!t || !old) return
  const gen = generation
  try {
    const mic = await captureMicrophone(state.prefs.input, state.prefs.noiseGate)
    if (gen !== generation || microphone !== old) return mic.stop()
    mic.track.enabled = !muted()
    await t.publish('mic', mic.track)
    microphone = mic
    old.stop()
    set({ mic: mic.track })
  } catch (cause) {
    set({ error: captureError(cause, 'microphone') })
  }
}

export const setNoiseGate = (noiseGate: boolean) => {
  savePrefs({ noiseGate })
  void restartMicrophone()
}

export const setDevice = async (kind: 'input' | 'output' | 'camera', deviceId: string) => {
  savePrefs({ [kind]: deviceId })
  if (kind === 'input') return restartMicrophone()
  const old = state.camera
  if (kind !== 'camera' || !old || !transport) return
  const t = transport
  try {
    const track = await captureCamera(deviceId)
    if (state.camera !== old) return track.stop()
    track.addEventListener('ended', () => { if (state.camera === track) void setCamera(false) })
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
  set({ watching: { ...state.watching, [watchKey(userId, track)]: on } })
  transport.subscribe(userId, track, on)
}

export const dismissError = () => set({ error: null })
