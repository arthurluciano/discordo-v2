import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { useMobilePane } from './use-mobile-pane'

afterEach(() => window.history.replaceState(null, ''))

describe('useMobilePane', () => {
  it('comeca na navegacao', () => {
    const { result } = renderHook(() => useMobilePane())
    expect(result.current.pane).toBe('navigation')
  })

  describe('ao abrir o conteudo', () => {
    it('mostra o conteudo e empilha uma entrada no historico', () => {
      const { result } = renderHook(() => useMobilePane())
      const depth = window.history.length
      act(() => result.current.showContent())
      expect(result.current.pane).toBe('content')
      expect(window.history.length).toBe(depth + 1)
    })

    it('nao empilha de novo quando ja esta no conteudo', () => {
      const { result } = renderHook(() => useMobilePane())
      act(() => result.current.showContent())
      const depth = window.history.length
      act(() => result.current.showContent())
      expect(window.history.length).toBe(depth)
    })
  })

  describe('ao voltar', () => {
    it('volta para a navegacao pelo botao', async () => {
      const { result } = renderHook(() => useMobilePane())
      act(() => result.current.showContent())
      act(() => result.current.showNavigation())
      await waitFor(() => expect(result.current.pane).toBe('navigation'))
    })

    it('volta para a navegacao pelo gesto de voltar do sistema', async () => {
      const { result } = renderHook(() => useMobilePane())
      act(() => result.current.showContent())
      act(() => window.history.back())
      await waitFor(() => expect(result.current.pane).toBe('navigation'))
    })
  })
})
