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
