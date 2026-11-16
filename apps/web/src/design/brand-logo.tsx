import { motion } from 'motion/react'
import { cn } from '@/lib/utils'
import { QUICK_EASE, useTransition } from './motion'

const mascotLogoUrl = new URL('../assets/discordo-mascot.png', import.meta.url).href

type BrandLogoProps = { className?: string; label?: string; interactive?: boolean; variant?: 'mascot' | 'mark' }

export const BrandLogo = ({ className, label = 'Discordo', interactive = false }: BrandLogoProps) => {
  const transition = useTransition(QUICK_EASE)
  return (
    <motion.img
      src={mascotLogoUrl}
      alt={label}
      draggable={false}
      whileHover={interactive ? { scale: 1.05 } : {}}
      whileTap={interactive ? { scale: 0.95 } : {}}
      transition={transition}
      className={cn('block shrink-0 rounded-2xl object-cover', className)}
    />
  )
}
