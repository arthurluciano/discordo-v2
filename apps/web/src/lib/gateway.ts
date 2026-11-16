// The one WebSocket to the server. Authenticated by the session cookie; feeds the store, and hands
// call signaling to whoever listens (the voice session).
import { dispatch, setState } from './store'
import type { GatewayEvent } from './wire'

type Listener = (e: GatewayEvent) => void

const listeners = new Set<Listener>()
let socket: WebSocket | null = null
let stopped = true
let attempt = 0
let retry: ReturnType<typeof setTimeout> | undefined

const POLICY_VIOLATION = 1008 // the server's "unauthorized" close

/** Jittered exponential backoff, so that a deploy does not bring every client back at once. */
const backoff = () => Math.min(500 * 2 ** attempt++, 15_000) * (0.5 + Math.random() / 2)

const open = () => {
  const url = new URL('/gateway', window.location.href)
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
  const ws = new WebSocket(url)
  socket = ws
  ws.onopen = () => ws.send('{}') // the hello: no token, the cookie came with the upgrade
  ws.onmessage = (raw) => {
    const e = JSON.parse(String(raw.data)) as GatewayEvent
    if (e.t === 'READY') attempt = 0
    dispatch(e)
    listeners.forEach((l) => l(e))
  }
  ws.onclose = (close) => {
    if (socket !== ws) return
    socket = null
    if (close.code === POLICY_VIOLATION) {
      stopped = true
      setState((s) => ({ ...s, status: 'signed-out' }))
      return
    }
    if (stopped) return
    setState((s) => ({ ...s, status: 'offline' }))
    retry = setTimeout(open, backoff())
  }
}

export const startGateway = () => {
  if (!stopped) return
  stopped = false
  attempt = 0
  open()
}

export const stopGateway = () => {
  stopped = true
  clearTimeout(retry)
  socket?.close()
  socket = null
}

/** Sends a call operation (`voice_join`, `rtc_offer`, ...). Returns false when not connected. */
export const sendOp = (op: string, d: unknown = {}) => {
  if (socket?.readyState !== WebSocket.OPEN) return false
  socket.send(JSON.stringify({ op, d }))
  return true
}

/** Every event, after the store has applied it. */
export const onGatewayEvent = (listener: Listener) => {
  listeners.add(listener)
  return () => void listeners.delete(listener)
}
