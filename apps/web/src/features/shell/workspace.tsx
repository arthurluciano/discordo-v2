import { useEffect } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import { ErrorBoundary } from '@/design/error-boundary'
import { EDITORIAL_EASE, useTransition } from '@/design/motion'
import { ChannelChat } from '@/features/chat/channel-chat'
import { MemberList } from '@/features/members/member-list'
import { HomeContent } from '@/features/social/home-content'
import { HomeSidebar } from '@/features/social/home-sidebar'
import { GuildSidebar } from '@/features/spaces/guild-sidebar'
import { ServerRail } from '@/features/spaces/server-rail'
import { VoiceStage } from '@/features/voice/voice-stage'
import { useStore } from '@/lib/store'
import { cn } from '@/lib/utils'
import { ChannelType } from '@/lib/wire'
import { navigate, usePlace } from './navigation'
import { useMobilePane, usePaneSlide } from './use-mobile-pane'
import { FloatingDock, UserDock } from './user-dock'

const GuildMain = ({ guildId, channelId, onBack }: { guildId: string; channelId: string | null; onBack: () => void }) => {
  const channel = useStore((s) => s.guilds.find((g) => g.id === guildId)?.channels.find((c) => c.id === channelId))
  const transition = useTransition(EDITORIAL_EASE)

  return (
    <AnimatePresence mode="wait" initial={false}>
      <motion.div key={channel?.id ?? 'none'} initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} transition={transition} className="flex flex-1 overflow-hidden">
        {!channel ? (
          <div className="grid flex-1 place-items-center text-center">
            <div>
              <h2 className="mb-2 text-2xl font-bold text-snow">Selecione um canal</h2>
              <p className="text-greyple">Escolha um canal à esquerda para começar</p>
            </div>
          </div>
        ) : channel.type === ChannelType.Voice ? (
          <ErrorBoundary label="Chamada"><VoiceStage channelId={channel.id} onBack={onBack} /></ErrorBoundary>
        ) : (
          <>
            <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
              <ErrorBoundary label="Canal">
                <ChannelChat channelId={channel.id} guildId={guildId} title={channel.name} topic={channel.topic} onBack={onBack} />
              </ErrorBoundary>
            </div>
            <div className="hidden w-60 shrink-0 border-l border-white/5 lg:block">
              <ErrorBoundary label="Membros"><MemberList guildId={guildId} /></ErrorBoundary>
            </div>
          </>
        )}
      </motion.div>
    </AnimatePresence>
  )
}

/** Server rail, the sidebar of the current place (with the user dock), and what is open in it. */
export const Workspace = () => {
  const place = usePlace()
  const guildGone = useStore((s) => place.kind === 'guild' && s.status === 'ready' && !s.guilds.some((g) => g.id === place.guildId))
  const { pane, showContent, showNavigation } = useMobilePane()
  const paneScope = usePaneSlide<HTMLDivElement>(pane)
  const sidebarTransition = useTransition(EDITORIAL_EASE)

  // Left (or was removed from) the guild being viewed.
  useEffect(() => {
    if (guildGone) navigate.home()
  }, [guildGone])

  useEffect(() => {
    if (place.content) showContent()
    else showNavigation()
  }, [place, showContent, showNavigation])

  const back = () => (place.kind === 'guild' ? navigate.guild(place.guildId) : navigate.home())
  const sidebarKey = place.kind === 'guild' ? place.guildId : 'home'

  return (
    <>
      <div ref={paneScope} className="flex h-full overflow-hidden bg-surface-content">
        <div className={cn('flex h-full min-w-0 shrink-0', pane === 'content' ? 'max-md:hidden' : 'max-md:flex-1')}>
          <ServerRail activeGuildId={place.kind === 'guild' ? place.guildId : null} />
          <motion.aside key={sidebarKey} initial={{ opacity: 0, x: -8 }} animate={{ opacity: 1, x: 0 }} transition={sidebarTransition} className="flex h-full min-w-0 flex-1 flex-col border-r border-white/10 bg-surface-secondary md:w-60 md:flex-none">
            <div className="dock-clearance flex min-h-0 flex-1 flex-col">
              <ErrorBoundary label="Barra lateral">
                {place.kind === 'guild' ? <GuildSidebar guildId={place.guildId} channelId={place.channelId} /> : <HomeSidebar dmId={place.dmId} />}
              </ErrorBoundary>
            </div>
            <UserDock />
          </motion.aside>
        </div>

        <main className={cn('flex min-w-0 flex-1 flex-col overflow-hidden', pane === 'navigation' && 'max-md:hidden')}>
          {place.kind === 'guild' ? (
            <GuildMain guildId={place.guildId} channelId={place.channelId} onBack={back} />
          ) : (
            <ErrorBoundary label="Início"><HomeContent dmId={place.dmId} onBack={back} /></ErrorBoundary>
          )}
        </main>
      </div>
      <FloatingDock visible={pane === 'navigation'} />
    </>
  )
}
