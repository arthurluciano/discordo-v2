type NavigatorFingerprint = Pick<Navigator, 'userAgent' | 'platform' | 'maxTouchPoints'>

const APPLE_MOBILE_USER_AGENT = /iPad|iPhone|iPod/

const IOS_PLATFORM = 'ios'

/** iPadOS se apresenta como Mac desde o 13; o que o denuncia e a tela de toque. */
export const isAppleMobile = ({ userAgent, platform, maxTouchPoints }: NavigatorFingerprint) =>
  APPLE_MOBILE_USER_AGENT.test(userAgent) || (platform === 'MacIntel' && maxTouchPoints > 1)

/** Liga a variante `ios:` do Tailwind (ver tokens.css) antes do primeiro render. */
export const markPlatform = (root: HTMLElement, fingerprint: NavigatorFingerprint) => {
  if (isAppleMobile(fingerprint)) root.dataset.platform = IOS_PLATFORM
}
