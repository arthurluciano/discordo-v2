import { describe, expect, it } from 'vitest'
import type { WorldMap } from '@/lib/wire'
import { gain, RADIUS, SPEED, step, zoneAt } from './logic'

const map: WorldMap = {
  width: 1000,
  height: 800,
  spawn: { x: 500, y: 400 },
  zones: [
    {
      id: 'cinema',
      name: 'Cinema',
      kind: 'cinema',
      status: 'No cinema',
      color: 0,
      x: 100,
      y: 100,
      w: 200,
      h: 200,
      door: 'top',
      spot: { x: 200, y: 200 },
    },
  ],
  walls: [{ x: 600, y: 0, w: 16, h: 800 }],
}

describe('step', () => {
  it('walks at the same speed straight and diagonally', () => {
    expect(step({ x: 100, y: 400 }, { x: 1, y: 0 }, 1, [], map)).toEqual({ x: 100 + SPEED, y: 400 })

    const diagonal = step({ x: 100, y: 400 }, { x: 1, y: 1 }, 1, [], map)
    expect(Math.hypot(diagonal.x - 100, diagonal.y - 400)).toBeCloseTo(SPEED)
  })

  it('stays put without a direction', () => {
    const pos = { x: 100, y: 400 }
    expect(step(pos, { x: 0, y: 0 }, 1, [], map)).toBe(pos)
  })

  it('stops at a wall but keeps sliding along it', () => {
    const next = step({ x: 575, y: 400 }, { x: 1, y: 1 }, 0.05, map.walls, map)
    expect(next.x).toBe(575)
    expect(next.y).toBeGreaterThan(400)
  })

  it('stays inside the map', () => {
    expect(step({ x: 30, y: 30 }, { x: -1, y: -1 }, 1, [], map)).toEqual({ x: RADIUS, y: RADIUS })
  })
})

describe('zoneAt', () => {
  it('finds the room of a point, edges as the server does', () => {
    expect(zoneAt(map, 100, 100)?.id).toBe('cinema')
    expect(zoneAt(map, 300, 150)).toBeUndefined()
    expect(zoneAt(map, 500, 400)).toBeUndefined()
  })
})

describe('gain', () => {
  it('fades with distance in the plaza', () => {
    expect(gain({ x: 0, y: 0 }, { x: 120, y: 0 })).toBe(1)
    expect(gain({ x: 0, y: 0 }, { x: 260, y: 0 })).toBeCloseTo(0.5)
    expect(gain({ x: 0, y: 0 }, { x: 0, y: 400 })).toBe(0)
    expect(gain({}, { x: 900, y: 900 })).toBe(0)
  })

  it('is all or nothing with a room involved', () => {
    expect(gain({ x: 0, y: 0, zone: 'a' }, { x: 900, y: 900, zone: 'a' })).toBe(1)
    expect(gain({ x: 0, y: 0, zone: 'a' }, { x: 1, y: 1, zone: 'b' })).toBe(0)
    expect(gain({ x: 0, y: 0, zone: 'a' }, { x: 1, y: 1 })).toBe(0)
    expect(gain({ x: 0, y: 0, zone: '' }, { x: 1, y: 1 })).toBe(1)
  })
})
