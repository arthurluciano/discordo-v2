import { useEffect, useRef, useState, type ReactNode } from 'react'
import { WifiOff, X } from 'lucide-react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { ErrorBoundary } from '@/design/error-boundary'
import { LoginScreen } from '@/features/auth/login-screen'
import { ProfileViewerProvider } from '@/features/identity/profile-viewer'
import { navigate } from '@/features/shell/navigation'
import { Workspace } from '@/features/shell/workspace'
import { VoiceProvider } from '@/features/voice/voice-session'
import { api, errorMessage } from '@/lib/api'
import { startGateway, stopGateway } from '@/lib/gateway'
import { clearPendingInvite, takePendingInvite } from '@/lib/pending-invite'
import { useStore } from '@/lib/store'

const Loading = ({ label }: { label: string }) => (
  <div className="grid h-full place-items-center bg-surface-content">
    <div className="flex items-center gap-3 text-sm font-semibold text-fog"><Spinner aria-hidden />{label}</div>
  </div>
)

/** `fixed inset-0` instead of `h-dvh`: on iOS with a translucent status bar, 100dvh leaves a strip at the bottom. */
const AppFrame = ({ children }: { children: ReactNode }) => (
  <div className="fixed inset-0 flex flex-col overflow-hidden bg-surface-rail pt-[env(safe-area-inset-top)] pr-[env(safe-area-inset-right)] pl-[env(safe-area-inset-left)]">
    <div className="min-h-0 flex-1">{children}</div>
  </div>
)

/** Uses an invite link (/invite/CODE) once there is a session, then opens that server. */
const usePendingInvite = (ready: boolean) => {
  const [code] = useState(takePendingInvite)
  const [error, setError] = useState<string | null>(null)
  const tried = useRef(false)
  useEffect(() => {
    if (!ready || !code || tried.current) return
    tried.current = true
    api.joinInvite(code)
      .then((guild) => navigate.guild(guild.id))
      .catch((cause: unknown) => setError(`Não foi possível usar o convite: ${errorMessage(cause)}`))
      .finally(clearPendingInvite)
  }, [ready, code])
  return { error, dismiss: () => setError(null) }
}

const Banner = ({ children, onDismiss }: { children: ReactNode; onDismiss?: () => void }) => (
  <Alert variant="destructive" className="m-2 shrink-0">
    <AlertDescription className="flex items-center justify-between gap-3">
      <span className="flex items-center gap-2">{children}</span>
      {onDismiss ? <Button type="button" variant="ghost" size="icon-sm" onClick={onDismiss} aria-label="Dispensar"><X aria-hidden /></Button> : null}
    </AlertDescription>
  </Alert>
)

export const App = () => {
  const status = useStore((s) => s.status)
  const hasSession = useStore((s) => s.me !== null)
  const invite = usePendingInvite(status === 'ready')

  useEffect(() => {
    startGateway()
    return stopGateway
  }, [])

  if (status === 'signed-out') return <AppFrame><LoginScreen /></AppFrame>
  if (!hasSession) return <AppFrame><Loading label="Conectando ao Discordo" /></AppFrame>

  return (
    <AppFrame>
      <ErrorBoundary label="Aplicação">
        <ProfileViewerProvider>
          <VoiceProvider>
            <div className="flex h-full flex-col">
              {status === 'offline' ? <Banner><WifiOff aria-hidden className="size-4" />Sem conexão. Reconectando…</Banner> : null}
              {invite.error ? <Banner onDismiss={invite.dismiss}>{invite.error}</Banner> : null}
              <div className="min-h-0 flex-1"><Workspace /></div>
            </div>
          </VoiceProvider>
        </ProfileViewerProvider>
      </ErrorBoundary>
    </AppFrame>
  )
}
