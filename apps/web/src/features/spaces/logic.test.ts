import { describe, expect, it } from 'vitest'
import { Permission } from '@/lib/permissions'
import type { Role } from '@/lib/wire'
import { bitState, moveRole, parseInviteCode, setBitState, sortRoles } from './logic'

const { ViewChannel: V, SendMessages: S } = Permission
const role = (id: string, position: number): Role => ({ id, guild_id: 'g', name: id, color: 0, permissions: 0, position })

describe('parseInviteCode', () => {
  it('takes bare codes and links', () => {
    expect(parseInviteCode('  ABC234XYZ7 ')).toBe('ABC234XYZ7')
    expect(parseInviteCode('https://discordo.app/invite/ABC234XYZ7')).toBe('ABC234XYZ7')
    expect(parseInviteCode('localhost:5173/invite/abc-1/')).toBe('abc-1')
  })
  it('rejects anything else', () => {
    expect(parseInviteCode('')).toBeNull()
    expect(parseInviteCode('https://example.com/other')).toBeNull()
    expect(parseInviteCode('two words')).toBeNull()
  })
})

describe('overwrite tri-state', () => {
  it('reads each bit', () => {
    const o = { allow: V, deny: S }
    expect(bitState(o, V)).toBe('allow')
    expect(bitState(o, S)).toBe('deny')
    expect(bitState(o, Permission.Connect)).toBe('inherit')
    expect(bitState(undefined, V)).toBe('inherit')
  })
  it('sets one bit without touching the others, never both allow and deny', () => {
    const o = { allow: V, deny: S }
    expect(setBitState(o, S, 'allow')).toEqual({ allow: V | S, deny: 0 })
    expect(setBitState(o, V, 'deny')).toEqual({ allow: 0, deny: V | S })
    expect(setBitState(o, V, 'inherit')).toEqual({ allow: 0, deny: S })
    expect(setBitState(undefined, V, 'deny')).toEqual({ allow: 0, deny: V })
  })
})

describe('roles', () => {
  it('sorts highest first, ties by creation', () => {
    expect(sortRoles([role('a', 1), role('g', 0), role('c', 3), role('b', 1)]).map((r) => r.id)).toEqual(['c', 'a', 'b', 'g'])
  })
  it('swaps distinct positions and breaks ties by raising whoever ends on top', () => {
    expect(moveRole(role('a', 1), role('b', 3), true)).toEqual([['a', 3], ['b', 1]])
    expect(moveRole(role('b', 1), role('a', 1), true)).toEqual([['b', 2]])
    expect(moveRole(role('a', 1), role('b', 1), false)).toEqual([['b', 2]])
  })
})
