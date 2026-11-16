import { useSyncExternalStore } from 'react'

/** Tailwind's `md` breakpoint: below it the layout shows one pane at a time. */
export const MOBILE_QUERY = '(max-width: 767px)'

const mobileMedia = () => (typeof window.matchMedia === 'function' ? window.matchMedia(MOBILE_QUERY) : null)

const subscribe = (onChange: () => void) => {
  const media = mobileMedia()
  media?.addEventListener('change', onChange)

  return () => media?.removeEventListener('change', onChange)
}

const isMobileNow = () => mobileMedia()?.matches ?? false

export const useIsMobile = () => useSyncExternalStore(subscribe, isMobileNow)
