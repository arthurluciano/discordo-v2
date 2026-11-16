// The member list beside a guild channel.
import { useMemo } from 'react'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Avatar } from '@/design/avatar'
import { useStore } from '@/lib/store'
import { cn } from '@/lib/utils'
import { useProfileViewer } from '../identity/profile-viewer'
import { displayName } from '../identity/users'

export const MemberList = ({ guildId }: { guildId: string }) => {
  const guild = useStore((s) => s.guilds.find((g) => g.id === guildId))
  const { openProfile } = useProfileViewer()

  const groups = useMemo(() => {
    const colored = [...(guild?.roles ?? [])].filter((r) => r.color).sort((a, b) => b.position - a.position)
    const members = (guild?.members ?? [])
      .map((m) => {
        const color = colored.find((r) => m.roles.includes(r.id))?.color
        return { ...m, name: displayName(m.user, m.nickname), color: color ? `#${color.toString(16).padStart(6, '0')}` : undefined }
      })
      .sort((a, b) => a.name.localeCompare(b.name, 'pt-BR'))
    return [
      { label: 'Online', online: true, members: members.filter((m) => m.online) },
      { label: 'Offline', online: false, members: members.filter((m) => !m.online) },
    ]
  }, [guild])

  return (
    <ScrollArea className="h-full bg-surface-sidebar">
      <div className="space-y-6 p-3 pt-6">
        {groups.map((group) => group.members.length ? (
          <section key={group.label}>
            <h2 className="section-label mb-2 px-2">{group.label} — {group.members.length}</h2>
            <ul className="space-y-0.5">
              {group.members.map((m) => (
                <li key={m.user.id} className={cn(!group.online && 'opacity-45')}>
                  <button type="button" onClick={() => openProfile(m.user.id)} className="flex w-full items-center gap-3 rounded-md px-2 py-1.5 text-left text-sm font-medium text-greyple hover:bg-surface-hover hover:text-fog">
                    <Avatar userId={m.user.id} displayName={m.name} avatarUrl={m.user.avatar_url} state={group.online ? 'online' : 'offline'} ringClass="ring-surface-sidebar" />
                    <span className="truncate" style={{ color: m.color }}>{m.name}</span>
                  </button>
                </li>
              ))}
            </ul>
          </section>
        ) : null)}
      </div>
    </ScrollArea>
  )
}
