import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AppUpdatePrompt } from './app-update-prompt'

const update = vi.hoisted(() => ({ available: false, apply: vi.fn() }))

vi.mock('@/lib/service-worker', () => ({
  subscribeToAppUpdate: () => () => undefined,
  isAppUpdateAvailable: () => update.available,
  applyAppUpdate: update.apply,
}))

beforeEach(() => {
  update.available = false
})

describe('AppUpdatePrompt', () => {
  it('fica escondido sem versao nova', () => {
    render(<AppUpdatePrompt />)
    expect(screen.queryByText('Nova versão disponível')).not.toBeInTheDocument()
  })

  describe('com versao nova esperando', () => {
    beforeEach(() => {
      update.available = true
    })

    it('anuncia a atualizacao', () => {
      render(<AppUpdatePrompt />)
      expect(screen.getByRole('status')).toHaveTextContent('Nova versão disponível')
    })

    it('aplica a atualizacao ao clicar em Atualizar', async () => {
      render(<AppUpdatePrompt />)
      await userEvent.click(screen.getByRole('button', { name: 'Atualizar' }))
      expect(update.apply).toHaveBeenCalled()
      expect(screen.getByRole('button', { name: 'Atualizando…' })).toBeDisabled()
    })

    it('some ao dispensar', async () => {
      render(<AppUpdatePrompt />)
      await userEvent.click(screen.getByRole('button', { name: 'Dispensar aviso de atualização' }))
      await vi.waitFor(() => expect(screen.queryByText('Nova versão disponível')).not.toBeInTheDocument())
    })
  })
})
