// What a profile looks like: the profile viewer shows it, and the profile settings preview it while editing.
import type { ReactNode } from 'react'
import { Avatar } from '@/design/avatar'
import { useStore } from '@/lib/store'
import type { UserProfile } from '@/lib/wire'
import { displayName } from './users'

export const MAX_BIO = 190

export const MAX_PRONOUNS = 40

/** 0 is the theme's own banner. */
export const bannerColor = (color: number) => (color ? `#${color.toString(16).padStart(6, '0')}` : null)

export const memberSince = (createdAt: string) =>
  new Date(createdAt).toLocaleDateString('pt-BR', { day: 'numeric', month: 'short', year: 'numeric' })

const Block = ({ title, children }: { title: string; children: ReactNode }) => (
  <section className="space-y-1">
    <h3 className="text-xs font-bold tracking-wide text-snow uppercase">{title}</h3>
    {children}
  </section>
)

/** The servers in common, by name (only the ones this client knows). */
const MutualServers = ({ ids }: { ids: string[] }) => {
  // Joined inside the selector: a string compares equal between renders, a new array would not.
  const names = useStore((s) =>
    s.guilds
      .filter((g) => ids.includes(g.id))
      .map((g) => g.name)
      .join(', '),
  )

  return names ? <p className="text-sm text-fog">{names}</p> : null
}

type ProfileCardViewProps = {
  /** Without the details (still loading) the card shows just the name. */
  profile: Pick<UserProfile, 'id' | 'username' | 'display_name' | 'avatar_url'> & Partial<UserProfile>
  online?: boolean
  /** Next to the avatar: edit, for our own card. */
  corner?: ReactNode
  /** Under everything: message, friend request. */
  actions?: ReactNode
}

export const ProfileCardView = ({ profile, online = false, corner, actions }: ProfileCardViewProps) => {
  const banner = bannerColor(profile.banner_color ?? 0)
  const mutual = profile.mutual_guild_ids ?? []

  return (
    <div className="overflow-hidden rounded-xl bg-popover">
      <div
        className={banner ? 'h-24' : 'h-24 bg-gradient-to-br from-blurple/60 to-fuchsia/40'}
        style={banner ? { background: banner } : undefined}
      />

      <div className="px-5 pb-5">
        <div className="-mt-10 mb-3 flex items-end justify-between gap-3">
          <span className="rounded-full bg-popover ring-4 ring-popover">
            <Avatar
              userId={profile.id}
              displayName={displayName(profile)}
              avatarUrl={profile.avatar_url}
              size="large"
              state={online ? 'online' : 'offline'}
              ringClass="ring-popover"
            />
          </span>

          {corner}
        </div>

        <h2 className="text-lg leading-tight font-bold break-words text-snow">{displayName(profile)}</h2>

        <p className="text-sm font-medium text-greyple">
          @{profile.username}
          {profile.pronouns ? <span> · {profile.pronouns}</span> : null}
        </p>

        <div className="mt-4 space-y-4 rounded-lg bg-surface-floating/60 p-3 empty:hidden">
          {profile.bio ? (
            <Block title="Sobre mim">
              <p className="text-sm whitespace-pre-wrap text-fog">{profile.bio}</p>
            </Block>
          ) : null}

          {profile.created_at ? (
            <Block title="Membro do Discordo desde">
              <p className="text-sm text-fog">{memberSince(profile.created_at)}</p>
            </Block>
          ) : null}

          {mutual.length > 0 ? (
            <Block title={mutual.length === 1 ? 'Servidor em comum' : `${mutual.length} servidores em comum`}>
              <MutualServers ids={mutual} />
            </Block>
          ) : null}
        </div>

        {actions ? (
          <div className="mt-4 flex flex-wrap items-center gap-2 border-t border-border pt-4">{actions}</div>
        ) : null}
      </div>
    </div>
  )
}
