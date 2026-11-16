const SERVICE_WORKER_URL = '/sw.js'

const SKIP_WAITING_MESSAGE = { type: 'SKIP_WAITING' } as const

/** O PWA instalado costuma ficar aberto por dias: sem checagem periodica, so veria o deploy novo
 * ao ser fechado e reaberto. */
const UPDATE_CHECK_INTERVAL_MS = 60 * 60 * 1000

let waitingWorker: ServiceWorker | null = null

const updateListeners = new Set<() => void>()

const publishWaitingWorker = (worker: ServiceWorker) => {
  waitingWorker = worker
  for (const listener of updateListeners) listener()
}

export const subscribeToAppUpdate = (listener: () => void) => {
  updateListeners.add(listener)

  return () => {
    updateListeners.delete(listener)
  }
}

export const isAppUpdateAvailable = () => waitingWorker !== null

/** Ativa a versao que esta esperando e recarrega quando ela assume a pagina. */
export const applyAppUpdate = () => {
  if (!waitingWorker) return
  navigator.serviceWorker.addEventListener('controllerchange', () => window.location.reload(), { once: true })
  waitingWorker.postMessage(SKIP_WAITING_MESSAGE)
}

/** Sem `controller` e a primeira instalacao, nao uma atualizacao — nada a oferecer. */
const trackInstallingWorker = (registration: ServiceWorkerRegistration) => {
  const installing = registration.installing
  if (!installing) return
  installing.addEventListener('statechange', () => {
    if (installing.state === 'installed' && navigator.serviceWorker.controller) publishWaitingWorker(installing)
  })
}

const checkForUpdate = (registration: ServiceWorkerRegistration) => {
  registration.update().catch((error: unknown) => console.warn('[pwa] falha ao procurar atualizacao', error))
}

/** Em dev o Vite serve modulos sem hash e o SW atrapalharia o HMR; no shell Tauri o WKWebView
 * nao precisa de instalacao nem offline. */
export const registerServiceWorker = async () => {
  if (!import.meta.env.PROD || !('serviceWorker' in navigator)) return
  const registration = await navigator.serviceWorker.register(SERVICE_WORKER_URL)

  if (registration.waiting && navigator.serviceWorker.controller) publishWaitingWorker(registration.waiting)
  registration.addEventListener('updatefound', () => trackInstallingWorker(registration))

  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') checkForUpdate(registration)
  })
  window.setInterval(() => checkForUpdate(registration), UPDATE_CHECK_INTERVAL_MS)
}
