import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

/**
 * Bun injeta o proprio `localStorage` global (um shim sem `.clear()`) antes do
 * ambiente jsdom do vitest montar o dele — os dois ficam no mesmo objeto, e o
 * shim do Bun vence. Substitui por uma implementacao real de Storage.
 */
class MemoryStorage implements Storage {
  #data = new Map<string, string>()

  get length() {
    return this.#data.size
  }

  clear = () => this.#data.clear()
  getItem = (key: string) => this.#data.get(key) ?? null
  key = (index: number) => [...this.#data.keys()][index] ?? null
  removeItem = (key: string) => void this.#data.delete(key)
  setItem = (key: string, value: string) => void this.#data.set(key, String(value))
}

for (const target of [globalThis, window]) {
  Object.defineProperty(target, 'localStorage', { value: new MemoryStorage(), configurable: true })
  Object.defineProperty(target, 'sessionStorage', { value: new MemoryStorage(), configurable: true })
}

afterEach(cleanup)
