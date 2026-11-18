// The guild's world: a call drawn as a map, where you hear who is near you or in your room.
import { useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import type { MotionValue } from 'motion/react'
import { useShallow } from 'zustand/react/shallow'
import { ArrowLeft, Globe, MapPin, Maximize2 } from 'lucide-react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { displayName, useMe } from '@/features/identity/users'
import { callPermissions } from '@/features/voice/call'
import { useAudioLevel } from '@/features/voice/use-audio-level'
import { ControlsBar } from '@/features/voice/voice-controls'
import { useVoice, voice } from '@/features/voice/voice-session'
import { isTyping, VideoView } from '@/features/voice/voice-stage'
import { sendOp } from '@/lib/gateway'
import { has, Permission } from '@/lib/permissions'
import { getState, setState, useStore } from '@/lib/store'
import { cn } from '@/lib/utils'
import type { Point, VoiceState } from '@/lib/wire'
import { facingOf, step, walkStep, zoneAt, type Walk } from './logic'
import { useWorldMap } from './use-world-map'

type WorldViewProps = {
  channelId: string
  guildId: string
  /** Mobile: back to the navigation pane. */
  onBack: () => void
}

/** The server drops more than 25 moves a second; 10 is plenty for others to interpolate. */
const SEND_MS = 100

/** Someone who moved further than this between two updates teleported: no gliding through walls. */
const TELEPORT = 300

/** Slower than this (map units per second) is standing: the end of a glide must not keep the legs going. */
const WALKING = 30

/** A frame of the character sheet: 3 columns (standing, two steps) by 4 rows (see `facingOf`). */
const FRAME_W = 60

const FRAME_H = 78

/** The feet are this far above the frame's bottom edge. */
const FEET = 6

const STEP_MS = 150

/** The map's grass, for what the view shows past its edges. */
const GRASS = '#8ccf7e'

const KEYS: Record<string, 'up' | 'down' | 'left' | 'right'> = {
  w: 'up',
  arrowup: 'up',
  s: 'down',
  arrowdown: 'down',
  a: 'left',
  arrowleft: 'left',
  d: 'right',
  arrowright: 'right',
}

const images = new Map<string, HTMLImageElement>()

/** Null until it has loaded (or if it never does). */
const imageOf = (url: string) => {
  let image = images.get(url)
  if (!image) {
    image = new Image()
    image.src = url
    images.set(url, image)
  }

  return image.complete && image.naturalWidth > 0 ? image : null
}

const SPEAKING = '#57f287'

const OFF = '#ed4245'

/** The name above a character, its pointer's tip at (x, y): a status mark and the name on a dark pill. */
const drawPill = (
  ctx: CanvasRenderingContext2D,
  font: string,
  x: number,
  y: number,
  name: string,
  state: VoiceState,
  speaking: boolean,
) => {
  ctx.font = `600 12px ${font}`
  const width = Math.ceil(ctx.measureText(name).width) + 32
  const left = Math.round(x - width / 2)
  const top = y - 25

  ctx.fillStyle = 'rgba(17, 18, 20, 0.85)'
  ctx.beginPath()
  ctx.roundRect(left, top, width, 20, 10)
  ctx.moveTo(x - 5, y - 5)
  ctx.lineTo(x, y)
  ctx.lineTo(x + 5, y - 5)
  ctx.fill()

  const mx = left + 13
  const my = top + 10
  ctx.lineWidth = 1.5
  ctx.lineCap = 'round'
  ctx.strokeStyle = ctx.fillStyle = OFF
  ctx.beginPath()

  if (state.deafened) {
    // Headphones: the band, then the two cups.
    ctx.arc(mx, my + 1, 4, Math.PI, 0)
    ctx.stroke()
    ctx.fillRect(mx - 5, my + 1, 2.5, 4)
    ctx.fillRect(mx + 2.5, my + 1, 2.5, 4)
  } else if (state.muted) {
    // A microphone: the capsule, its cradle and the stem.
    ctx.roundRect(mx - 1.5, my - 5, 3, 6, 1.5)
    ctx.fill()
    ctx.beginPath()
    ctx.arc(mx, my, 3.5, 0, Math.PI)
    ctx.moveTo(mx, my + 3.5)
    ctx.lineTo(mx, my + 5.5)
    ctx.stroke()
  } else {
    ctx.fillStyle = speaking ? SPEAKING : '#3ba55d'
    ctx.shadowColor = SPEAKING
    ctx.shadowBlur = speaking ? 6 : 0
    ctx.arc(mx, my, 4, 0, Math.PI * 2)
    ctx.fill()
    ctx.shadowBlur = 0

    if (speaking) {
      ctx.globalAlpha = 0.6
      ctx.strokeStyle = SPEAKING
      ctx.beginPath()
      ctx.arc(mx, my, 6, 0, Math.PI * 2)
      ctx.stroke()
      ctx.globalAlpha = 1
    }
  }

  if (state.deafened || state.muted) {
    ctx.beginPath()
    ctx.moveTo(mx - 5, my + 5)
    ctx.lineTo(mx + 5, my - 5)
    ctx.stroke()
  }

  ctx.fillStyle = '#fff'
  ctx.textAlign = 'left'
  ctx.textBaseline = 'middle'
  ctx.fillText(name, left + 24, my + 0.5)
}

/** Where the view starts on one axis: centered on `center`, never past the map; a map smaller than the view is centered. */
const cameraAxis = (center: number, view: number, size: number) =>
  size <= view ? (size - view) / 2 : Math.max(0, Math.min(center - view / 2, size - view))

type Levels = Map<string, MotionValue<number>>

/** Feeds someone's speaking level (the same one the call stage uses) to the canvas. */
const Level = ({ userId, track, levels }: { userId: string; track: MediaStreamTrack | null; levels: Levels }) => {
  const level = useAudioLevel(track)

  useEffect(() => {
    levels.set(userId, level)

    return () => void levels.delete(userId)
  }, [userId, level, levels])

  return null
}

const peopleCount = (n: number) => `${n} ${n === 1 ? 'pessoa' : 'pessoas'}`

export const WorldView = ({ channelId, guildId, onBack }: WorldViewProps) => {
  const map = useWorldMap()
  const me = useMe()
  const meId = me?.id ?? ''
  const guild = useStore((s) => s.guilds.find((g) => g.id === guildId))
  const perms = useStore((s) => callPermissions(s, channelId))

  const v = useVoice((s) => ({
    channelId: s.channelId,
    connection: s.connection,
    remote: s.remote,
    mic: s.mic,
    screen: s.screen,
    error: s.error,
  }))
  const ours = v.channelId === channelId
  const inCall = ours && (v.connection === 'connecting' || v.connection === 'connected')
  const connected = ours && v.connection === 'connected'

  // Narrow selectors: positions change ten times a second, none of these do.
  const ids = useStore(
    useShallow((s) =>
      Object.values(s.voice)
        .filter((o) => o.channel_id === channelId)
        .map((o) => o.user_id),
    ),
  )
  const myZone = useStore((s) => {
    const mine = s.voice[meId]

    return (mine?.channel_id === channelId && mine.zone) || ''
  })

  const cinema = map?.zones.find((z) => z.kind === 'cinema' && z.screen)
  const inCinema = inCall && !!cinema && myZone === cinema.id

  /** Who is on the cinema's screen: of those sharing in there, the first by ID (it may be us). */
  const screening = useStore((s) =>
    cinema
      ? (Object.values(s.voice)
          .filter((o) => o.channel_id === channelId && o.zone === cinema.id && o.tracks.includes('screen'))
          .map((o) => o.user_id)
          .sort()[0] ?? null)
      : null,
  )
  /** Only those inside watch it. */
  const sharerId = inCinema ? screening : null
  const video =
    sharerId === null
      ? null
      : sharerId === meId
        ? v.screen
        : (v.remote.find((r) => r.userId === sharerId && r.name === 'screen')?.track ?? null)

  const [expanded, setExpanded] = useState(false)
  const [levels] = useState<Levels>(() => new Map())
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const screenRef = useRef<HTMLDivElement>(null)
  /** Where we stand: ours to decide, the server only follows. Null while not in this world. */
  const pos = useRef<Point | null>(null)
  /** Where a click told us to walk to. */
  const target = useRef<Walk | null>(null)
  const keys = useRef(new Set<string>())
  const camera = useRef<Point>({ x: 0, y: 0 })
  /** Where everyone is drawn (the others on their way to where they are) and which way they face. */
  const shown = useRef(new Map<string, Point & { facing: number }>())

  const people = useMemo(() => {
    const list = new Map<string, string>()
    for (const id of ids) {
      const member = guild?.members.find((m) => m.user.id === id)
      list.set(id, member ? displayName(member.user, member.nickname) : 'Alguém')
    }

    return list
  }, [ids, guild])

  useEffect(() => {
    if (!connected || !sharerId || sharerId === meId) return
    voice.watch(sharerId, 'screen', true)

    return () => voice.watch(sharerId, 'screen', false)
  }, [connected, sharerId, meId])

  // Sharing is the cinema's: walking out ends ours. (Without the map we do not know where we are yet.)
  const sharing = v.screen !== null
  useEffect(() => {
    if (map && ours && sharing && !inCinema) void voice.stopScreen()
  }, [map, ours, sharing, inCinema])

  useEffect(() => {
    if (!inCall) return
    const held = keys.current

    const down = (e: KeyboardEvent) => {
      const key = KEYS[e.key.toLowerCase()]
      if (!key || e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey) return
      // Menus and dialogs use the arrows themselves.
      if (isTyping(e.target) || (e.target instanceof Element && e.target.closest('[role="menu"], [role="dialog"]')))
        return

      e.preventDefault() // the arrows would scroll
      held.add(key)
      target.current = null
    }

    const up = (e: KeyboardEvent) => {
      const key = KEYS[e.key.toLowerCase()]
      if (key) held.delete(key)
    }

    const clear = () => held.clear()

    window.addEventListener('keydown', down)
    window.addEventListener('keyup', up)
    window.addEventListener('blur', clear)

    return () => {
      window.removeEventListener('keydown', down)
      window.removeEventListener('keyup', up)
      window.removeEventListener('blur', clear)
      held.clear()
    }
  }, [inCall])

  useEffect(() => {
    const canvas = canvasRef.current
    const ctx = canvas?.getContext('2d')
    if (!canvas || !ctx || !map) return

    if (!inCall) pos.current = target.current = null

    // The skin can change the font.
    const readFont = () => getComputedStyle(document.body).fontFamily || 'sans-serif'
    let font = readFont()
    const observer = new MutationObserver(() => (font = readFont()))
    observer.observe(document.documentElement, { attributes: true })

    let last = performance.now()
    let sentAt = 0
    let frame = 0

    const walk = (now: number, dt: number, mine: VoiceState) => {
      // The server omits a coordinate at exactly 0.
      pos.current ??= { x: mine.x ?? 0, y: mine.y ?? 0 }
      let p = pos.current

      const held = keys.current
      const dir = {
        x: Number(held.has('right')) - Number(held.has('left')),
        y: Number(held.has('down')) - Number(held.has('up')),
      }

      if (dir.x || dir.y) {
        p = step(p, dir, dt, map.walls, map)
      } else if (target.current) {
        const walked = walkStep(p, target.current, now, dt, map.walls, map)
        p = walked.pos
        target.current = walked.walk
      }
      pos.current = p

      // One rule covers walking (10 Hz), the last position after stopping, and "ir para". Comparing with the
      // store also puts back a position that a late VOICE_STATE_UPDATE of ours (a mute, a room change) overwrote.
      if ((p.x === (mine.x ?? 0) && p.y === (mine.y ?? 0)) || now - sentAt < SEND_MS) return
      sentAt = now
      sendOp('world_move', p)

      // The server does not echo our own moves, and the call's audio reads where we are from the store.
      const zone = zoneAt(map, p.x, p.y)?.id ?? ''
      setState((s) => {
        const state = s.voice[meId]

        return state ? { ...s, voice: { ...s.voice, [meId]: { ...state, x: p.x, y: p.y, zone } } } : s
      })
    }

    const draw = (now: number) => {
      frame = requestAnimationFrame(draw)
      const dt = Math.min((now - last) / 1000, 0.05) // a long pause must not jump through a wall
      last = now

      const dpr = window.devicePixelRatio || 1
      const width = canvas.clientWidth
      const height = canvas.clientHeight
      if (canvas.width !== Math.round(width * dpr) || canvas.height !== Math.round(height * dpr)) {
        canvas.width = Math.round(width * dpr)
        canvas.height = Math.round(height * dpr)
      }

      const voiceStates = getState().voice
      const mine = inCall ? voiceStates[meId] : undefined
      if (mine?.channel_id === channelId) walk(now, dt, mine)

      // Whole device pixels, or the art blurs and shimmers as the camera moves.
      const snap = (value: number) => Math.round(value * dpr) / dpr

      const focus = pos.current ?? map.spawn
      const cam = {
        x: snap(cameraAxis(focus.x, width, map.width)),
        y: snap(cameraAxis(focus.y, height, map.height)),
      }
      camera.current = cam

      const { screen } = cinema ?? {}
      if (screen && screenRef.current)
        screenRef.current.style.transform = `translate(${screen.x - cam.x}px, ${screen.y - cam.y}px)`

      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
      ctx.imageSmoothingEnabled = false // resizing the canvas turns it back on
      ctx.fillStyle = GRASS
      ctx.fillRect(0, 0, width, height)
      ctx.translate(-cam.x, -cam.y)

      const art = imageOf('/world/map.png')
      if (art) ctx.drawImage(art, 0, 0, map.width, map.height)

      const at = shown.current
      for (const id of at.keys()) if (voiceStates[id]?.channel_id !== channelId) at.delete(id)

      const characters = Object.values(voiceStates)
        .filter((o) => o.channel_id === channelId)
        .map((state) => {
          const id = state.user_id
          const own = id === meId ? pos.current : null
          const to = own ?? { x: state.x ?? 0, y: state.y ?? 0 }
          const from = at.get(id) ?? { ...to, facing: 0 }

          const jumped = Math.hypot(to.x - from.x, to.y - from.y) > TELEPORT
          const t = own || jumped ? 1 : 1 - Math.exp(-dt * 12)
          const dx = (to.x - from.x) * t
          const dy = (to.y - from.y) * t
          const walking = !jumped && Math.hypot(dx, dy) > WALKING * dt

          const p = { x: from.x + dx, y: from.y + dy, facing: walking ? facingOf(dx, dy) : from.facing }
          at.set(id, p)

          return { state, p, walking }
        })
        .sort((a, b) => a.p.y - b.p.y)

      const stepFrame = 1 + (Math.floor(now / STEP_MS) % 2)
      for (const { state, p, walking } of characters) {
        const sheet = imageOf(`/world/sprites/${encodeURIComponent(state.user_id)}`)
        if (!sheet) continue

        ctx.drawImage(
          sheet,
          (walking ? stepFrame : 0) * FRAME_W,
          p.facing * FRAME_H,
          FRAME_W,
          FRAME_H,
          snap(p.x - FRAME_W / 2),
          snap(p.y - FRAME_H + FEET),
          FRAME_W,
          FRAME_H,
        )
      }

      // The names go over every character, as nobody's should hide behind someone else.
      for (const { state, p } of characters) {
        const name = people.get(state.user_id)
        const speaking = (levels.get(state.user_id)?.get() ?? 0) > 0
        if (name) drawPill(ctx, font, snap(p.x), snap(p.y - FRAME_H + FEET), name, state, speaking)
      }
    }

    frame = requestAnimationFrame(draw)

    return () => {
      cancelAnimationFrame(frame)
      observer.disconnect()
    }
  }, [map, cinema, channelId, meId, inCall, people, levels])

  const walkTo = (e: ReactPointerEvent<HTMLCanvasElement>) => {
    if (!inCall) return
    const box = e.currentTarget.getBoundingClientRect()
    const to = { x: e.clientX - box.left + camera.current.x, y: e.clientY - box.top + camera.current.y }
    target.current = { to, best: Infinity, since: performance.now() }
  }

  const goTo = (p: Point) => {
    pos.current = p
    target.current = null
  }

  const zoneName = map?.zones.find((z) => z.id === myZone)?.name ?? 'Praça'

  return (
    <section
      aria-label="Mundo"
      className="relative flex h-full min-h-0 flex-1 flex-col gap-3 bg-surface-rail p-3 sm:p-4"
    >
      <header className="flex shrink-0 items-center gap-1.5">
        <Button variant="ghost" size="icon-sm" className="md:hidden" onClick={onBack} aria-label="Voltar">
          <ArrowLeft aria-hidden />
        </Button>
        <div className="min-w-0 flex-1">
          <h2 className="truncate font-bold text-snow">Mundo</h2>
          <p className="telemetry truncate text-greyple" data-numeric>
            {peopleCount(ids.length)}
          </p>
        </div>
        {inCall && map ? (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="sm">
                <MapPin aria-hidden />
                Ir para…
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-56">
              <DropdownMenuItem onSelect={() => goTo(map.spawn)}>Praça</DropdownMenuItem>
              {map.zones.map((z) => (
                <DropdownMenuItem key={z.id} onSelect={() => goTo(z.spot)}>
                  {z.name}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        ) : null}
      </header>

      {inCall ? (
        <p className="sr-only" aria-live="polite">
          Você está em: {zoneName}
        </p>
      ) : null}

      <div className="relative min-h-0 flex-1 overflow-hidden rounded-2xl">
        <canvas
          ref={canvasRef}
          role="img"
          aria-label={
            inCall
              ? 'Mapa do mundo. Ande com W, A, S, D ou as setas, clique no mapa, ou use o menu Ir para.'
              : 'Mapa do mundo'
          }
          onPointerDown={walkTo}
          className="absolute inset-0 size-full touch-none"
        />
        {cinema?.screen ? (
          <div
            ref={screenRef}
            className={cn(
              'absolute left-0 top-0',
              video ? 'bg-black' : 'pointer-events-none flex flex-col items-center justify-center gap-1 text-center',
            )}
            style={{ width: cinema.screen.w, height: cinema.screen.h }}
          >
            {video ? (
              <>
                <VideoView track={video} contain mirror={false} />
                <Button
                  variant="outline"
                  size="icon-sm"
                  aria-label="Ampliar o telão"
                  onClick={() => setExpanded(true)}
                  className="absolute right-2 top-2 border-white/10 bg-black/60 text-white backdrop-blur-sm hover:bg-black/80"
                >
                  <Maximize2 aria-hidden />
                </Button>
              </>
            ) : (
              <>
                <p className="text-sm font-semibold text-white/60">
                  {screening ? 'Transmissão em andamento' : 'Ninguém transmitindo'}
                </p>
                <p className="text-xs text-white/40">
                  {screening
                    ? inCinema
                      ? 'Carregando…'
                      : 'Entre no cinema para assistir.'
                    : inCinema
                      ? 'Use o botão de compartilhar tela para transmitir aqui.'
                      : 'Entre no cinema para compartilhar sua tela.'}
                </p>
              </>
            )}
          </div>
        ) : null}
      </div>

      {ours && v.error ? (
        <Alert variant="destructive" className="shrink-0">
          <AlertDescription className="flex items-center gap-2">
            <span className="flex-1">{v.error}</span>
            <Button variant="ghost" size="xs" onClick={voice.dismissError}>
              Fechar
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}

      <footer className="flex shrink-0 justify-center">
        {inCall ? (
          <ControlsBar channelId={channelId} camera={false} share={inCinema} />
        ) : has(perms, Permission.Connect) ? (
          <Button size="lg" onClick={() => voice.join(channelId)}>
            <Globe aria-hidden />
            Entrar no mundo
          </Button>
        ) : (
          <p className="text-sm text-greyple">Você não tem permissão para entrar no mundo.</p>
        )}
      </footer>

      {inCall
        ? ids.map((id) => (
            <Level
              key={id}
              userId={id}
              track={id === meId ? v.mic : (v.remote.find((r) => r.userId === id && r.name === 'mic')?.track ?? null)}
              levels={levels}
            />
          ))
        : null}

      <Dialog open={expanded && video !== null} onOpenChange={setExpanded}>
        <DialogContent className="bg-surface-floating sm:max-w-5xl">
          <DialogHeader>
            <DialogTitle>Telão do cinema</DialogTitle>
            <DialogDescription className="sr-only">A tela compartilhada no cinema, ampliada.</DialogDescription>
          </DialogHeader>
          <div className="aspect-video overflow-hidden rounded-lg bg-black">
            {video ? <VideoView track={video} contain mirror={false} /> : null}
          </div>
        </DialogContent>
      </Dialog>
    </section>
  )
}
