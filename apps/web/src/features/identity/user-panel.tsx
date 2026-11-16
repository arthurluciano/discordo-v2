import { useState } from 'react'
import { motion } from 'motion/react'
import { LogOut, Palette } from 'lucide-react'
import { ThemeDialog } from '@/components/theme-dialog'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { Avatar } from '@/design/avatar'
import { QUICK_EASE, useTransition } from '@/design/motion'
import { useSignOut } from '../auth/use-sign-out'
import { useProfileViewer } from './profile-viewer'
import { displayName, useMe } from './users'

/** The bottom of the sidebar: who you are, your profile, appearance, signing out. */
export const UserPanel = () => {
  const me = useMe()
  const { openProfile } = useProfileViewer()
  const signOut = useSignOut()
  const press = useTransition(QUICK_EASE)
  const [appearance, setAppearance] = useState(false)
  if (!me) return null

  return (
    <div className="flex h-14 items-center gap-2 px-2 text-fog max-md:h-16 max-md:px-3">
      <motion.button
        type="button"
        whileHover={{ x: 1 }}
        whileTap={{ scale: 0.97 }}
        transition={press}
        onClick={() => openProfile(me.id)}
        className="flex min-w-0 flex-1 items-center gap-2 rounded-md text-left"
      >
        <Avatar
          userId={me.id}
          displayName={displayName(me)}
          avatarUrl={me.avatar_url}
          state="online"
          ringClass="ring-surface-rail"
        />
        <span className="min-w-0 flex-1 leading-tight">
          <span className="block truncate text-xs font-semibold text-snow">{displayName(me)}</span>
          <span className="block truncate text-[11px] text-greyple">@{me.username}</span>
        </span>
      </motion.button>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            onClick={() => setAppearance(true)}
            aria-label="Aparência"
          >
            <Palette aria-hidden />
          </Button>
        </TooltipTrigger>
        <TooltipContent>Aparência</TooltipContent>
      </Tooltip>
      <ThemeDialog open={appearance} onOpenChange={setAppearance} />
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            onClick={() => signOut.mutate()}
            disabled={signOut.isPending}
            aria-label="Sair"
          >
            <LogOut aria-hidden />
          </Button>
        </TooltipTrigger>
        <TooltipContent>Sair</TooltipContent>
      </Tooltip>
    </div>
  )
}
