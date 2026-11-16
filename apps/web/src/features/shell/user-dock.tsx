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

/**
 * Perfil + controles de voz, sempre no mesmo lugar visual — servidor ou area social. Quem esta
 * numa chamada precisa continuar vendo os proprios controles ao navegar pro Inicial, do mesmo
 * jeito que a chamada em si continua tocando (ver `VoiceAudio`).
 *
 * No desktop mora no pe da barra lateral; no celular quem aparece e o `FloatingDock`.
 */
export const UserDock = () => {
  const isMobile = useIsMobile()
  if (isMobile) return null
  return (
    <div className="border-t border-white/10 bg-surface-rail glass:m-2 glass:mt-0 glass:rounded-2xl glass:border-0 glass:bg-surface-raised">
      <DockContents />
    </div>
  )
}

/** Publica a altura do dock para o `dock-clearance` das listas: com a chamada de voz aberta ele
 * cresce, e uma folga fixa deixaria controles por cima do ultimo canal. */
const useDockHeight = () => {
  const dockRef = useRef<HTMLDivElement>(null)

  useLayoutEffect(() => {
    const dock = dockRef.current
    if (!dock || typeof ResizeObserver === 'undefined') return
    const root = document.documentElement
    const observer = new ResizeObserver(([entry]) => {
      if (entry) root.style.setProperty(DOCK_HEIGHT_PROPERTY, `${entry.borderBoxSize[0]?.blockSize ?? dock.offsetHeight}px`)
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

/** Dock do celular: flutua acima da barra de gestos (no iOS, em vidro) e so aparece nas telas
 * de navegacao — dentro de um canal ou conversa ele sai de cena deslizando para baixo. */
export const FloatingDock = ({ visible }: { visible: boolean }) => {
  const isMobile = useIsMobile()
  return <AnimatePresence>{isMobile && visible ? <FloatingDockPanel key="floating-dock" /> : null}</AnimatePresence>
}
