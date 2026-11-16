import { useLayoutEffect, useRef } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import { SPRING_SOFT, useTransition } from '@/design/motion'
import { UserPanel } from '@/features/identity/user-panel'
import { VoiceDock } from '@/features/voice/voice-dock'
import { useIsMobile } from './use-is-mobile'

const DOCK_HEIGHT_PROPERTY = '--dock-height'

const DOCK_HIDDEN = { opacity: 0, y: 32, scale: 0.94 }

const DOCK_SHOWN = { opacity: 1, y: 0, scale: 1 }

const DockContents = () => (
  <>
    <VoiceDock />
    <UserPanel />
  </>
)

/** Profile and call controls in the same spot on every screen, so a call stays controllable while navigating.
 * At the foot of the sidebar on wide screens; phones get the `FloatingDock`. */
export const UserDock = () => {
  const isMobile = useIsMobile()
  if (isMobile) return null

  return (
    <div className="border-t border-white/10 bg-surface-rail glass:m-2 glass:mt-0 glass:rounded-2xl glass:border-0 glass:bg-surface-raised">
      <DockContents />
    </div>
  )
}

/** Publishes the dock's height for the lists' `dock-clearance`: it grows during a call, so a fixed gap would hide the last item. */
const useDockHeight = () => {
  const dockRef = useRef<HTMLDivElement>(null)

  useLayoutEffect(() => {
    const dock = dockRef.current
    if (!dock || typeof ResizeObserver === 'undefined') return
    const root = document.documentElement
    const observer = new ResizeObserver(([entry]) => {
      if (entry)
        root.style.setProperty(DOCK_HEIGHT_PROPERTY, `${entry.borderBoxSize[0]?.blockSize ?? dock.offsetHeight}px`)
    })
    observer.observe(dock)
    return () => {
      observer.disconnect()
      root.style.removeProperty(DOCK_HEIGHT_PROPERTY)
    }
  }, [])

  return dockRef
}

const FloatingDockPanel = () => {
  const dockRef = useDockHeight()
  const transition = useTransition(SPRING_SOFT)

  return (
    <motion.div
      ref={dockRef}
      initial={DOCK_HIDDEN}
      animate={DOCK_SHOWN}
      exit={DOCK_HIDDEN}
      transition={transition}
      style={{ originY: 1 }}
      className="fixed inset-x-3 bottom-[calc(env(safe-area-inset-bottom)+0.75rem)] z-40 overflow-hidden rounded-2xl border border-white/10 bg-surface-floating/95 shadow-2xl ios:rounded-[1.75rem] ios:dock-glass glass:glass-panel glass:glass-float glass:rounded-[1.75rem]"
    >
      <DockContents />
    </motion.div>
  )
}

/** The phone dock: floats above the gesture bar and only shows on navigation screens (it slides away inside a chat). */
export const FloatingDock = ({ visible }: { visible: boolean }) => {
  const isMobile = useIsMobile()

  return <AnimatePresence>{isMobile && visible ? <FloatingDockPanel key="floating-dock" /> : null}</AnimatePresence>
}
