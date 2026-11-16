import { motion } from 'motion/react'
import { Avatar as AvatarRoot, AvatarFallback, AvatarImage } from '@/components/ui/avatar'
import { cn } from '@/lib/utils'
import { SPRING_SNAP } from './motion'

type PresenceState = 'online' | 'away' | 'offline'

const SIZES = { small: 'size-8 text-xs', medium: 'size-10 text-sm', large: 'size-20 text-2xl' } as const
const DOT_POSITION = { small: 'size-3 -right-0.5 -bottom-0.5', medium: 'size-3.5 -right-0.5 -bottom-0.5', large: 'size-5 right-0 bottom-0' } as const
const COLORS = ['bg-blurple', 'bg-cerulean', 'bg-fuchsia', 'bg-ember-orange', 'bg-spring-green'] as const
const initialOf = (displayName: string) => (displayName.trim()[0] ?? '?').toUpperCase()
const colorOf = (userId: string) => COLORS[[...userId].reduce((sum, char) => sum + char.charCodeAt(0), 0) % COLORS.length]

type AvatarProps = { userId: string; displayName: string; avatarUrl?: string | null | undefined; size?: keyof typeof SIZES; state?: PresenceState | undefined; ringClass?: string }

export const Avatar = ({ userId, displayName, avatarUrl, size = 'small', state, ringClass = 'ring-surface-sidebar' }: AvatarProps) => (
  <span className="relative shrink-0" data-user={userId} aria-label={`${displayName}${state ? `, ${state === 'online' ? 'online' : state === 'away' ? 'ausente' : 'offline'}` : ''}`}>
    <AvatarRoot className={cn('bg-surface-raised', SIZES[size])}>
      {avatarUrl ? <AvatarImage src={avatarUrl} alt="" /> : null}
      <AvatarFallback className={cn('font-bold text-white', colorOf(userId))}>{initialOf(displayName)}</AvatarFallback>
    </AvatarRoot>
    {state ? <motion.span aria-hidden layout initial={{ scale: 0, opacity: 0 }} animate={{ scale: 1, opacity: 1 }} transition={SPRING_SNAP} className={cn('absolute rounded-full ring-[3px] glass:ring-(--canvas)', state === 'online' ? 'bg-spring-green' : state === 'away' ? 'bg-ember-orange' : 'bg-greyple', DOT_POSITION[size], ringClass)} /> : null}
  </span>
)
