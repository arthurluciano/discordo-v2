// The guild's world: a call drawn as a map, where you hear who is near you or in your room.
import { useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import type { MotionValue } from 'motion/react'
import { useShallow } from 'zustand/react/shallow'
import { ArrowLeft, Globe, MapPin, Maximize2 } from 'lucide-react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { colorOf, initialOf } from '@/design/avatar'
import { displayName, useMe } from '@/features/identity/users'
import { callPermissions } from '@/features/voice/call'
import { useAudioLevel } from '@/features/voice/use-audio-level'
import { ControlsBar } from '@/features/voice/voice-controls'
import { useVoice, voice } from '@/features/voice/voice-session'
import { isTyping, VideoView } from '@/features/voice/voice-stage'
import { sendOp } from '@/lib/gateway'
import { has, Permission } from '@/lib/permissions'
import { getState, setState, useStore } from '@/lib/store'
import type { Point, VoiceState, WorldMap } from '@/lib/wire'
import { RADIUS, SPEED, step, zoneAt } from './logic'
import { useWorldMap } from './use-world-map'

type WorldViewProps = {
  channelId: string
  guildId: string
  /** Mobile: back to the navigation pane. */
  onBack: () => void
}

type Person = { name: string; avatarUrl: string; color: string }

/** The server drops more than 25 moves a second; 10 is plenty for others to interpolate. */
const SEND_MS = 100

const GRID = 100

/** Someone who moved further than this between two updates teleported: no gliding through walls. */
const TELEPORT = 300

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

/** The theme's colors, for the canvas: read again whenever the skin or theme changes. */
const readColors = () => {
  const css = getComputedStyle(document.documentElement)
  const cache = new Map<string, string>()

  const token = (name: string) => {
    let value = cache.get(name)
    if (value === undefined) cache.set(name, (value = css.getPropertyValue(name).trim()))

    return value
  }

  return { token, font: getComputedStyle(document.body).fontFamily || 'sans-serif' }
}

type Colors = ReturnType<typeof readColors>

const images = new Map<string, HTMLImageElement>()

/** Null until it has loaded (or if it never does): the initial is drawn meanwhile. */
const imageOf = (url: string) => {
  let image = images.get(url)
  if (!image) {
    image = new Image()
    image.src = url
    images.set(url, image)
  }

  return image.complete && image.naturalWidth > 0 ? image : null
}

const hex = (color: number) => `#${color.toString(16).padStart(6, '0')}`

const drawMap = (ctx: CanvasRenderingContext2D, c: Colors, map: WorldMap) => {
  ctx.fillStyle = c.token('--surface-rail')
  ctx.fillRect(0, 0, map.width, map.height)

  ctx.globalAlpha = 0.08
  ctx.strokeStyle = c.token('--color-greyple')
  ctx.lineWidth = 1
  ctx.beginPath()
  for (let x = 0; x <= map.width; x += GRID) {
    ctx.moveTo(x, 0)
    ctx.lineTo(x, map.height)
  }
  for (let y = 0; y <= map.height; y += GRID) {
    ctx.moveTo(0, y)
    ctx.lineTo(map.width, y)
  }
  ctx.stroke()

  ctx.textAlign = 'center'
  ctx.textBaseline = 'middle'
  ctx.font = `700 18px ${c.font}`
  for (const z of map.zones) {
    ctx.globalAlpha = 0.18
    ctx.fillStyle = hex(z.color)
    ctx.fillRect(z.x, z.y, z.w, z.h)

    ctx.globalAlpha = 1
    ctx.fillStyle = c.token('--color-snow')
    ctx.fillText(z.name, z.x + z.w / 2, z.y + 48)

    if (z.screen) {
      ctx.fillStyle = '#000'
      ctx.fillRect(z.screen.x, z.screen.y, z.screen.w, z.screen.h)
      ctx.strokeStyle = c.token('--color-fog')
      ctx.lineWidth = 4
      ctx.strokeRect(z.screen.x, z.screen.y, z.screen.w, z.screen.h)
    }
  }

  ctx.globalAlpha = 1
  ctx.fillStyle = c.token('--color-fog')
  for (const w of map.walls) ctx.fillRect(w.x, w.y, w.w, w.h)
}

const drawAvatar = (
  ctx: CanvasRenderingContext2D,
  c: Colors,
  p: Point,
  person: Person,
  state: VoiceState,
  level: number,
) => {
  if (level > 0) {
    ctx.strokeStyle = c.token('--media-speaking')
    ctx.lineWidth = 2 + level * 4
    ctx.beginPath()
    ctx.arc(p.x, p.y, RADIUS + 2 + ctx.lineWidth / 2, 0, Math.PI * 2)
    ctx.stroke()
  }

  const image = person.avatarUrl ? imageOf(person.avatarUrl) : null
  ctx.save()
  ctx.beginPath()
  ctx.arc(p.x, p.y, RADIUS, 0, Math.PI * 2)
  ctx.clip()
  if (image) {
    ctx.drawImage(image, p.x - RADIUS, p.y - RADIUS, RADIUS * 2, RADIUS * 2)
  } else {
    ctx.fillStyle = c.token(person.color)
    ctx.fillRect(p.x - RADIUS, p.y - RADIUS, RADIUS * 2, RADIUS * 2)
    ctx.fillStyle = '#fff'
    ctx.font = `700 18px ${c.font}`
    ctx.textBaseline = 'middle'
    ctx.fillText(initialOf(person.name), p.x, p.y + 1)
  }
  ctx.restore()

  ctx.font = `600 12px ${c.font}`
  ctx.textBaseline = 'top'
  ctx.fillStyle = c.token('--color-snow')
  ctx.fillText(person.name, p.x, p.y + RADIUS + 6)

  if (state.muted) {
    const x = p.x + 15
    const y = p.y + 15
    ctx.fillStyle = c.token('--media-muted')
    ctx.beginPath()
    ctx.arc(x, y, 7, 0, Math.PI * 2)
    ctx.fill()
    ctx.strokeStyle = '#fff'
    ctx.lineWidth = 2
    ctx.beginPath()
    ctx.moveTo(x - 3, y - 3)
    ctx.lineTo(x + 3, y + 3)
    ctx.stroke()
  }
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
  const sharerId = useStore((s) =>
    inCinema
      ? (Object.values(s.voice)
          .filter((o) => o.channel_id === channelId && o.zone === myZone && o.tracks.includes('screen'))
          .map((o) => o.user_id)
          .sort()[0] ?? null)
      : null,
  )
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
  const target = useRef<Point | null>(null)
  const keys = useRef(new Set<string>())
  const camera = useRef<Point>({ x: 0, y: 0 })
  /** Where the others are drawn, on their way to where they are. */
  const shown = useRef(new Map<string, Point>())

  const people = useMemo(() => {
    const list = new Map<string, Person>()
    for (const id of ids) {
      const member = guild?.members.find((m) => m.user.id === id)
      list.set(id, {
        name: member ? displayName(member.user, member.nickname) : 'Alguém',
        avatarUrl: member?.user.avatar_url ?? '',
        color: `--color-${(colorOf(id) ?? 'bg-blurple').slice(3)}`,
      })
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

    let colors = readColors()
    const observer = new MutationObserver(() => (colors = readColors()))
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
      const to = target.current

      if (dir.x || dir.y) {
        p = step(p, dir, dt, map.walls, map)
      } else if (to) {
        const time = Math.min(dt, Math.hypot(to.x - p.x, to.y - p.y) / SPEED)
        const next = step(p, { x: to.x - p.x, y: to.y - p.y }, time, map.walls, map)
        // Arrived, or something is in the way.
        if (time < dt || Math.hypot(next.x - p.x, next.y - p.y) < SPEED * time - 0.01) target.current = null
        p = next
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

      const focus = pos.current ?? map.spawn
      const cam = {
        x: Math.round(cameraAxis(focus.x, width, map.width)),
        y: Math.round(cameraAxis(focus.y, height, map.height)),
      }
      camera.current = cam

      const { screen } = cinema ?? {}
      if (screen && screenRef.current)
        screenRef.current.style.transform = `translate(${screen.x - cam.x}px, ${screen.y - cam.y}px)`

      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
      ctx.clearRect(0, 0, width, height)
      ctx.translate(-cam.x, -cam.y)
      drawMap(ctx, colors, map)

      const at = shown.current
      for (const id of at.keys()) if (voiceStates[id]?.channel_id !== channelId) at.delete(id)

      const avatars = Object.values(voiceStates)
        .filter((o) => o.channel_id === channelId)
        .map((state) => {
          if (state.user_id === meId && pos.current) return { state, p: pos.current }

          const to = { x: state.x ?? 0, y: state.y ?? 0 }
          const from = at.get(state.user_id) ?? to
          const t = Math.hypot(to.x - from.x, to.y - from.y) > TELEPORT ? 1 : 1 - Math.exp(-dt * 12)
          const p = { x: from.x + (to.x - from.x) * t, y: from.y + (to.y - from.y) * t }
          at.set(state.user_id, p)

          return { state, p }
        })
        .sort((a, b) => a.p.y - b.p.y)

      ctx.textAlign = 'center'
      for (const { state, p } of avatars) {
        const person = people.get(state.user_id)
        if (person) drawAvatar(ctx, colors, p, person, state, levels.get(state.user_id)?.get() ?? 0)
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
    target.current = { x: e.clientX - box.left + camera.current.x, y: e.clientY - box.top + camera.current.y }
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
        {cinema?.screen && video ? (
          <div
            ref={screenRef}
            className="absolute left-0 top-0 bg-black"
            style={{ width: cinema.screen.w, height: cinema.screen.h }}
          >
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
