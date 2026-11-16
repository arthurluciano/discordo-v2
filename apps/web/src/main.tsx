import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MotionConfig } from 'motion/react'
import { App } from './app'
import { TooltipProvider } from './components/ui/tooltip'
import { AppUpdatePrompt } from './features/shell/app-update-prompt'
import { markPlatform } from './lib/platform'
import { registerServiceWorker } from './lib/service-worker'
import './design/tokens.css'

// Only mutations use it: the server state lives in lib/store, fed by the gateway.
const queryClient = new QueryClient()

markPlatform(document.documentElement, navigator)

const container = document.getElementById('root')
if (!container) throw new Error('Elemento #root nao existe no index.html')

createRoot(container).render(
  <StrictMode>
    <MotionConfig reducedMotion="user">
      <TooltipProvider>
        <QueryClientProvider client={queryClient}>
          <App />
          <AppUpdatePrompt />
        </QueryClientProvider>
      </TooltipProvider>
    </MotionConfig>
  </StrictMode>,
)

registerServiceWorker().catch((error: unknown) => console.error('[pwa] falha ao registrar o service worker', error))
