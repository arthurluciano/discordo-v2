import { describe, expect, it } from 'vitest'
import type { Guild } from '@/lib/wire'
import { roleColors } from './role-color'

const role = (id: string, color: number, position: number) => ({ id, color, position }) as Guild['roles'][number]

const member = (id: string, roles: string[]) => ({ user: { id }, roles }) as unknown as Guild['members'][number]

describe('roleColors', () => {
  it('picks the highest colored role and skips uncolored ones', () => {
    const colors = roleColors({
      roles: [role('low', 0xff0000, 1), role('high', 0x0000ff, 5), role('top', 0, 9)],
      members: [member('a', ['low', 'high', 'top']), member('b', ['low']), member('c', ['top']), member('d', [])],
    })
    expect(colors.get('a')).toBe('#0000ff')
    expect(colors.get('b')).toBe('#ff0000')
    expect(colors.has('c')).toBe(false)
    expect(colors.has('d')).toBe(false)
  })
})
