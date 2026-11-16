import { useCallback, useEffect, useRef, useState } from 'react'
import { useAnimate, useReducedMotion } from 'motion/react'
import { EDITORIAL_EASE } from '@/design/motion'
import { MOBILE_QUERY } from './use-is-mobile'

export type MobilePane = 'navigation' | 'content'

const CONTENT_HISTORY_STATE = 'discordo:mobile-content'
const SLIDE_OFFSET_PX = 32

const paneFromHistory = (): MobilePane => window.history.state === CONTENT_HISTORY_STATE ? 'content' : 'navigation'

/**
 * No celular so cabe um painel por vez: a navegacao (rail + canais/conversas) ou o conteudo.
 * Entrar no conteudo empilha uma entrada no historico para o gesto de voltar do sistema — no PWA
 * instalado, sem isso, voltar fecharia o app em vez de retornar a lista.
 */
export const useMobilePane = () => {
  const [pane, setPane] = useState<MobilePane>('navigation')

  useEffect(() => {
    const syncWithHistory = () => setPane(paneFromHistory())
    window.addEventListener('popstate', syncWithHistory)
    return () => window.removeEventListener('popstate', syncWithHistory)
  }, [])

  const showContent = useCallback(() => {
    if (paneFromHistory() !== 'content') window.history.pushState(CONTENT_HISTORY_STATE, '')
    setPane('content')
  }, [])

  const showNavigation = useCallback(() => {
    if (paneFromHistory() === 'content') return window.history.back()
    setPane('navigation')
  }, [])

  return { pane, showContent, showNavigation }
}

/** Troca de painel no celular desliza no sentido da navegacao: entrar vem da direita, voltar da
 * esquerda. No desktop os dois paineis estao sempre na tela e nada se mexe. */
export const usePaneSlide = <Element extends HTMLElement>(pane: MobilePane) => {
  const [scope, animate] = useAnimate<Element>()
  const reducedMotion = useReducedMotion()
  const previousPane = useRef(pane)

  useEffect(() => {
    if (previousPane.current === pane) return
    previousPane.current = pane
    if (reducedMotion || !window.matchMedia?.(MOBILE_QUERY).matches) return
    const direction = pane === 'content' ? 1 : -1
    animate(scope.current, { x: [direction * SLIDE_OFFSET_PX, 0], opacity: [0.5, 1] }, EDITORIAL_EASE)
  }, [pane, reducedMotion, animate, scope])

  return scope
}
