import { describe, expect, it } from 'vitest'
import { ChannelType, RelationshipStatus, type Relationship, type User } from '@/lib/wire'
import { dmTitle, friendsIn } from './dm'

const user = (id: string, display_name = ''): User => ({ id, username: `u${id}`, display_name, avatar_url: '' })

describe('dmTitle', () => {
  it('names a 1:1 after the other person', () => {
    expect(
      dmTitle({ id: 'd', type: ChannelType.DM, name: '', recipients: [user('me', 'Eu'), user('b', 'Bia')] }, 'me'),
    ).toBe('Bia')
  })
  it('uses the group name, else the others joined', () => {
    const recipients = [user('me'), user('b', 'Bia'), user('c')]
    expect(dmTitle({ id: 'd', type: ChannelType.Group, name: 'Time', recipients }, 'me')).toBe('Time')
    expect(dmTitle({ id: 'd', type: ChannelType.Group, name: '', recipients }, 'me')).toBe('Bia, uc')
    expect(dmTitle({ id: 'd', type: ChannelType.Group, name: '', recipients: [user('me')] }, 'me')).toBe('Grupo vazio')
  })
})

describe('friendsIn', () => {
  const rel = (id: string, name: string, status: number, online = false): Relationship => ({
    user: user(id, name),
    status,
    online,
  })
  const all = [
    rel('1', 'Zé', RelationshipStatus.Friend, true),
    rel('2', 'Ana', RelationshipStatus.Friend),
    rel('3', 'Caio', RelationshipStatus.Outgoing),
    rel('4', 'Duda', RelationshipStatus.Incoming),
    rel('5', 'Eva', RelationshipStatus.Blocked),
  ]
  const names = (tab: Parameters<typeof friendsIn>[1]) => friendsIn(all, tab).map((r) => r.user.display_name)

  it('filters by tab', () => {
    expect(names('online')).toEqual(['Zé'])
    expect(names('all')).toEqual(['Ana', 'Zé'])
    expect(names('pending')).toEqual(['Duda', 'Caio'])
    expect(names('blocked')).toEqual(['Eva'])
  })
  it('does not mutate its input', () => {
    friendsIn(all, 'all')
    expect(all[0]!.user.id).toBe('1')
  })
})
