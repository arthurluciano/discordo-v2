import { describe, expect, it } from 'vitest'
import { isAppleMobile, markPlatform } from './platform'

const IPHONE = {
  userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15',
  platform: 'iPhone',
  maxTouchPoints: 5,
}

const IPAD_AS_MAC = {
  userAgent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15',
  platform: 'MacIntel',
  maxTouchPoints: 5,
}

const MAC = {
  userAgent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15',
  platform: 'MacIntel',
  maxTouchPoints: 0,
}

const ANDROID = {
  userAgent: 'Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36',
  platform: 'Linux armv8l',
  maxTouchPoints: 5,
}

describe('isAppleMobile', () => {
  it('reconhece o iPhone', () => expect(isAppleMobile(IPHONE)).toBe(true))
  it('reconhece o iPad que se apresenta como Mac', () => expect(isAppleMobile(IPAD_AS_MAC)).toBe(true))
  it('ignora o Mac sem tela de toque', () => expect(isAppleMobile(MAC)).toBe(false))
  it('ignora o Android', () => expect(isAppleMobile(ANDROID)).toBe(false))
})

describe('markPlatform', () => {
  it('marca a raiz no iOS', () => {
    const root = document.createElement('html')
    markPlatform(root, IPHONE)
    expect(root.dataset.platform).toBe('ios')
  })

  it('deixa a raiz sem marca fora do iOS', () => {
    const root = document.createElement('html')
    markPlatform(root, ANDROID)
    expect(root.dataset.platform).toBeUndefined()
  })
})
