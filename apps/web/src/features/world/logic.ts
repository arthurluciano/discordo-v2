// The world's rules that need no DOM: walking, which room a point is in, how loud someone sounds.
import type { Point, Rect, WorldMap } from '@/lib/wire'

export const RADIUS = 20

/** Pixels per second. */
export const SPEED = 260

/** In the plaza: full volume up to NEAR, silent from FAR. */
const NEAR = 120

const FAR = 400

const hits = (x: number, y: number, walls: Rect[]) =>
  walls.some((r) => {
    const dx = x - Math.max(r.x, Math.min(x, r.x + r.w))
    const dy = y - Math.max(r.y, Math.min(y, r.y + r.h))

    return dx * dx + dy * dy < RADIUS * RADIUS
  })

const clamp = (v: number, min: number, max: number) => Math.max(min, Math.min(v, max))

/** Where an avatar at `pos` is after walking `dt` seconds towards `dir`. Each axis moves on its own, so it slides along walls. */
export const step = (pos: Point, dir: Point, dt: number, walls: Rect[], map: { width: number; height: number }) => {
  const length = Math.hypot(dir.x, dir.y)
  if (!length || dt <= 0) return pos

  const distance = (SPEED * dt) / length
  let x = clamp(pos.x + dir.x * distance, RADIUS, map.width - RADIUS)
  if (hits(x, pos.y, walls)) x = pos.x

  let y = clamp(pos.y + dir.y * distance, RADIUS, map.height - RADIUS)
  if (hits(x, y, walls)) y = pos.y

  return { x, y }
}

/** A walk to a clicked point: `best` is the closest it got, at `since`. */
export type Walk = { to: Point; best: number; since: number }

/** Without getting closer for this long, a walk gives up. */
const STUCK_MS = 300

/** One frame of a walk. It slides along what is in the way; `walk` comes back null once it arrived or got stuck. */
export const walkStep = (
  pos: Point,
  walk: Walk,
  now: number,
  dt: number,
  walls: Rect[],
  map: { width: number; height: number },
): { pos: Point; walk: Walk | null } => {
  const dir = { x: walk.to.x - pos.x, y: walk.to.y - pos.y }
  let next = step(pos, dir, Math.min(dt, Math.hypot(dir.x, dir.y) / SPEED), walls, map)

  // Blocked on one axis, the other gets the whole speed: otherwise the slide slows to a crawl as it lines up.
  const free = { x: next.x === pos.x ? 0 : dir.x, y: next.y === pos.y ? 0 : dir.y }
  if (!free.x !== !free.y) next = step(pos, free, Math.min(dt, Math.hypot(free.x, free.y) / SPEED), walls, map)

  const left = Math.hypot(walk.to.x - next.x, walk.to.y - next.y)

  if (left < 1) return { pos: next, walk: null }
  if (left < walk.best - 0.5) return { pos: next, walk: { ...walk, best: left, since: now } }

  return { pos: next, walk: now - walk.since > STUCK_MS ? null : walk }
}

/** The sprite sheet's row for a movement: down, left, right, up; the larger axis wins. */
export const facingOf = (dx: number, dy: number) => (Math.abs(dx) > Math.abs(dy) ? (dx < 0 ? 1 : 2) : dy < 0 ? 3 : 0)

/** The room at a point, as the server computes it. */
export const zoneAt = (map: WorldMap, x: number, y: number) =>
  map.zones.find((z) => x >= z.x && x < z.x + z.w && y >= z.y && y < z.y + z.h)

type Standing = { x?: number; y?: number; zone?: string }

/** How loud `other` sounds to `me`, 0 to 1. A coordinate the server omitted is 0. */
export const gain = (me: Standing, other: Standing) => {
  if (me.zone || other.zone) return me.zone === other.zone ? 1 : 0

  const distance = Math.hypot((me.x ?? 0) - (other.x ?? 0), (me.y ?? 0) - (other.y ?? 0))

  return clamp((FAR - distance) / (FAR - NEAR), 0, 1)
}
