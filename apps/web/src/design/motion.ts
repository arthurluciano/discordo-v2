import { useReducedMotion, type Transition, type Variants } from 'motion/react'

export const SPRING_SNAP: Transition = { type: 'spring', stiffness: 500, damping: 40, mass: 0.7 }
export const SPRING_SOFT: Transition = { type: 'spring', stiffness: 300, damping: 32, mass: 0.85 }
export const EDITORIAL_EASE: Transition = { duration: 0.28, ease: [0.16, 1, 0.3, 1] }
export const QUICK_EASE: Transition = { duration: 0.16, ease: [0.16, 1, 0.3, 1] }
export const INSTANT: Transition = { duration: 0 }

export const FADE_UP: Variants = {
  hidden: { opacity: 0, y: 8 },
  visible: { opacity: 1, y: 0 },
  exit: { opacity: 0, y: -4 },
}
export const FADE: Variants = { hidden: { opacity: 0 }, visible: { opacity: 1 }, exit: { opacity: 0 } }
export const PANEL: Variants = {
  hidden: { opacity: 0, scale: 0.985 },
  visible: { opacity: 1, scale: 1 },
  exit: { opacity: 0, scale: 0.99 },
}

export const useTransition = (preferred: Transition): Transition => useReducedMotion() ? INSTANT : preferred
