import { describe, expect, it } from 'vitest'
import { ALL, Permission, compute } from './permissions'

const { ViewChannel, SendMessages, Administrator } = Permission

describe('compute', () => {
  const base = ViewChannel | SendMessages
  it('lets administrators do everything', () => {
    expect(compute(Administrator, 'g', 'u', [], [{ target_id: 'g', is_member: false, allow: 0, deny: ALL }])).toBe(ALL)
  })
  it('applies @everyone, then roles (allow wins over deny), then the member', () => {
    const overwrites = [
      { target_id: 'g', is_member: false, allow: 0, deny: SendMessages },
      { target_id: 'r1', is_member: false, allow: SendMessages, deny: 0 },
      { target_id: 'r2', is_member: false, allow: 0, deny: SendMessages },
    ]
    expect(compute(base, 'g', 'u', [], overwrites)).toBe(ViewChannel)
    expect(compute(base, 'g', 'u', ['r1', 'r2'], overwrites)).toBe(base)
    const muted = [...overwrites, { target_id: 'u', is_member: true, allow: 0, deny: SendMessages }]
    expect(compute(base, 'g', 'u', ['r1'], muted)).toBe(ViewChannel)
  })
  it('ignores overwrites for other members and roles', () => {
    const overwrites = [{ target_id: 'other', is_member: true, allow: 0, deny: ViewChannel }]
    expect(compute(base, 'g', 'u', [], overwrites)).toBe(base)
  })
})
