import type { Guild } from '@/lib/wire'

/** user ID -> CSS color of their highest-positioned colored role; members without one are absent. */
export const roleColors = (guild: Pick<Guild, 'roles' | 'members'>): Map<string, string> => {
  const colored = guild.roles.filter((r) => r.color).sort((a, b) => b.position - a.position)
  const colors = new Map<string, string>()
  for (const m of guild.members) {
    const color = colored.find((r) => m.roles.includes(r.id))?.color
    if (color) colors.set(m.user.id, `#${color.toString(16).padStart(6, '0')}`)
  }

  return colors
}
