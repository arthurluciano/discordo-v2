/**
 * Service worker do PWA. Escopo propositalmente estreito: so a casca da SPA e
 * os assets com hash do Vite. API, uploads e realtime passam direto — cachear
 * resposta autenticada aqui vazaria dados entre sessoes.
 *
 * `__BUILD_ID__` vira o hash do build (ver `serviceWorkerBuildId` no vite.config.ts): sem isso o
 * arquivo seria identico entre deploys e o navegador nunca enxergaria uma versao nova.
 */
const BUILD_ID = '__BUILD_ID__'
const SHELL_CACHE = `discordo-shell-${BUILD_ID}`
const ASSET_CACHE = `discordo-assets-${BUILD_ID}`
const SKIP_WAITING_MESSAGE = 'SKIP_WAITING'
const SHELL_URL = '/'
const HASHED_ASSET_PREFIX = '/assets/'
const CURRENT_CACHES = [SHELL_CACHE, ASSET_CACHE]

self.addEventListener('install', (event) => {
  event.waitUntil(caches.open(SHELL_CACHE).then((cache) => cache.add(SHELL_URL)))
})

/** A versao nova espera a pessoa pedir para atualizar: trocar o SW por baixo de uma aba aberta
 * misturaria JS antigo com assets novos. */
self.addEventListener('message', (event) => {
  if (event.data?.type === SKIP_WAITING_MESSAGE) self.skipWaiting()
})

self.addEventListener('activate', (event) => {
  const staleCaches = caches.keys().then((keys) => keys.filter((key) => !CURRENT_CACHES.includes(key)))
  event.waitUntil(
    staleCaches
      .then((keys) => Promise.all(keys.map((key) => caches.delete(key))))
      .then(() => self.clients.claim()),
  )
})

const isHtml = (response) => response.ok && (response.headers.get('content-type') ?? '').includes('text/html')

/** Rede primeiro: deploy novo aparece na hora. Offline, a ultima casca guardada sobe. */
const navigate = async (request) => {
  try {
    const response = await fetch(request)
    if (isHtml(response)) {
      const cache = await caches.open(SHELL_CACHE)
      await cache.put(SHELL_URL, response.clone())
    }
    return response
  } catch (error) {
    const shell = await caches.match(SHELL_URL)
    if (shell) return shell
    throw error
  }
}

/** Nome com hash nunca muda de conteudo: cache primeiro e para sempre. */
const hashedAsset = async (request) => {
  const cached = await caches.match(request)
  if (cached) return cached
  const response = await fetch(request)
  if (response.ok) {
    const cache = await caches.open(ASSET_CACHE)
    await cache.put(request, response.clone())
  }
  return response
}

self.addEventListener('fetch', (event) => {
  const { request } = event
  if (request.method !== 'GET') return
  const url = new URL(request.url)
  if (url.origin !== self.location.origin) return
  if (request.mode === 'navigate') return event.respondWith(navigate(request))
  if (url.pathname.startsWith(HASHED_ASSET_PREFIX)) return event.respondWith(hashedAsset(request))
})
