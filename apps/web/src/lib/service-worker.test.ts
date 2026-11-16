import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

type FakeWorker = EventTarget & { state: ServiceWorkerState; postMessage: ReturnType<typeof vi.fn> }

const fakeWorker = (state: ServiceWorkerState): FakeWorker => Object.assign(new EventTarget(), { state, postMessage: vi.fn() })

const fakeRegistration = (overrides: { waiting?: FakeWorker; installing?: FakeWorker } = {}) =>
  Object.assign(new EventTarget(), {
    waiting: overrides.waiting ?? null,
    installing: overrides.installing ?? null,
    update: vi.fn(async () => undefined),
  })

let registration = fakeRegistration()
const register = vi.fn(async () => registration)
const container = Object.assign(new EventTarget(), { register, controller: null as object | null })

/** O modulo guarda a versao em espera em estado de modulo: cada teste parte de uma copia limpa. */
const loadModule = async () => {
  vi.resetModules()
  return import('./service-worker')
}

beforeEach(() => {
  registration = fakeRegistration()
  container.controller = null
  Object.defineProperty(navigator, 'serviceWorker', { value: container, configurable: true })
  vi.stubEnv('PROD', true)
})

afterEach(() => {
  vi.unstubAllEnvs()
    Reflect.deleteProperty(navigator, 'serviceWorker')
})

describe('registerServiceWorker', () => {
  describe('no navegador em producao', () => {
    it('registra o service worker na raiz', async () => {
      const { registerServiceWorker } = await loadModule()
      await registerServiceWorker()
      expect(register).toHaveBeenCalledWith('/sw.js')
    })
  })

  describe('em desenvolvimento', () => {
    it('nao registra, para nao interferir no HMR', async () => {
      vi.stubEnv('PROD', false)
      const { registerServiceWorker } = await loadModule()
      await registerServiceWorker()
      expect(register).not.toHaveBeenCalled()
    })
  })

  describe('sem suporte a service worker', () => {
    it('nao quebra', async () => {
      Reflect.deleteProperty(navigator, 'serviceWorker')
      const { registerServiceWorker } = await loadModule()
      await expect(registerServiceWorker()).resolves.toBeUndefined()
    })
  })
})

describe('atualizacao do app', () => {
  describe('quando uma versao nova ja esta esperando ao abrir', () => {
    it('oferece a atualizacao', async () => {
      container.controller = {}
      registration = fakeRegistration({ waiting: fakeWorker('installed') })
      const { registerServiceWorker, isAppUpdateAvailable } = await loadModule()
      await registerServiceWorker()
      expect(isAppUpdateAvailable()).toBe(true)
    })
  })

  describe('quando uma versao nova termina de instalar com o app aberto', () => {
    it('avisa quem esta inscrito', async () => {
      container.controller = {}
      const installing = fakeWorker('installing')
      const { registerServiceWorker, isAppUpdateAvailable, subscribeToAppUpdate } = await loadModule()
      const listener = vi.fn()
      subscribeToAppUpdate(listener)
      await registerServiceWorker()

      registration.installing = installing
      registration.dispatchEvent(new Event('updatefound'))
      installing.state = 'installed'
      installing.dispatchEvent(new Event('statechange'))

      expect(listener).toHaveBeenCalled()
      expect(isAppUpdateAvailable()).toBe(true)
    })
  })

  describe('na primeira instalacao', () => {
    it('nao oferece atualizacao', async () => {
      const installing = fakeWorker('installing')
      const { registerServiceWorker, isAppUpdateAvailable } = await loadModule()
      await registerServiceWorker()

      registration.installing = installing
      registration.dispatchEvent(new Event('updatefound'))
      installing.state = 'installed'
      installing.dispatchEvent(new Event('statechange'))

      expect(isAppUpdateAvailable()).toBe(false)
    })
  })

  describe('ao aplicar', () => {
    it('pede para a versao em espera assumir', async () => {
      container.controller = {}
      const waiting = fakeWorker('installed')
      registration = fakeRegistration({ waiting })
      const { registerServiceWorker, applyAppUpdate } = await loadModule()
      await registerServiceWorker()

      applyAppUpdate()

      expect(waiting.postMessage).toHaveBeenCalledWith({ type: 'SKIP_WAITING' })
    })
  })

  describe('ao voltar para o app', () => {
    it('procura uma versao nova', async () => {
      const { registerServiceWorker } = await loadModule()
      await registerServiceWorker()
      document.dispatchEvent(new Event('visibilitychange'))
      expect(registration.update).toHaveBeenCalled()
    })
  })
})
