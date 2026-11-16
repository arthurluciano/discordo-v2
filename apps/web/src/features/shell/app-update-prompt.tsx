import { useState, useSyncExternalStore } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import { RefreshCw, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { SPRING_SOFT, useTransition } from '@/design/motion'
import { applyAppUpdate, isAppUpdateAvailable, subscribeToAppUpdate } from '@/lib/service-worker'

/** A new PWA version is ready. Dismissing only hides this until next launch; the update applies once every tab closes. */
export const AppUpdatePrompt = () => {
  const updateAvailable = useSyncExternalStore(subscribeToAppUpdate, isAppUpdateAvailable)
  const [dismissed, setDismissed] = useState(false)
  const [applying, setApplying] = useState(false)
  const transition = useTransition(SPRING_SOFT)

  const update = () => {
    setApplying(true)
    applyAppUpdate()
  }

  return (
    <AnimatePresence>
      {updateAvailable && !dismissed ? (
        <motion.div
          role="status"
          aria-live="polite"
          initial={{ opacity: 0, y: 24, scale: 0.96 }}
          animate={{ opacity: 1, y: 0, scale: 1 }}
          exit={{ opacity: 0, y: 16, scale: 0.98 }}
          transition={transition}
          className="fixed inset-x-4 bottom-[calc(1rem+env(safe-area-inset-bottom))] z-50 mx-auto flex max-w-sm items-center gap-3 rounded-xl border border-white/10 bg-surface-floating p-3 text-snow shadow-2xl glass:glass-panel glass:glass-float glass:rounded-2xl"
        >
          <span className="grid size-9 shrink-0 place-items-center rounded-full bg-blurple/15 text-blurple-soft">
            <RefreshCw aria-hidden className="size-4" />
          </span>
          <span className="min-w-0 flex-1 text-sm leading-tight">
            <span className="block font-semibold">Nova versão disponível</span>
            <span className="block text-xs text-greyple">Atualize para usar a mais recente.</span>
          </span>
          <Button type="button" size="sm" onClick={update} disabled={applying}>
            {applying ? 'Atualizando…' : 'Atualizar'}
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            onClick={() => setDismissed(true)}
            aria-label="Dispensar aviso de atualização"
          >
            <X aria-hidden />
          </Button>
        </motion.div>
      ) : null}
    </AnimatePresence>
  )
}
