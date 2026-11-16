import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ThemeProvider, useTheme } from './theme-provider'

const ThemeProbe = () => {
  const { theme, resolvedTheme, setTheme } = useTheme()
  return (
    <div>
      <span>{theme}:{resolvedTheme}</span>
      <button type="button" onClick={() => setTheme('dark')}>Escuro</button>
    </div>
  )
}

describe('ThemeProvider', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.classList.remove('dark')
    vi.stubGlobal('matchMedia', vi.fn().mockReturnValue({
      matches: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    }))
  })

  it('usa o tema do sistema por padrão e persiste uma escolha explícita', async () => {
    render(<ThemeProvider><ThemeProbe /></ThemeProvider>)
    expect(screen.getByText('system:light')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Escuro' }))

    await waitFor(() => expect(document.documentElement).toHaveClass('dark'))
    expect(screen.getByText('dark:dark')).toBeInTheDocument()
    expect(localStorage.getItem('discordo-theme')).toBe('dark')
  })
})
