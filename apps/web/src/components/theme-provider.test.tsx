import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { resolveTheme, ThemeProvider, useTheme } from './theme-provider'

const ThemeProbe = () => {
  const { skin, theme, resolvedTheme, setTheme, setSkin, setReduceTransparency } = useTheme()

  return (
    <div>
      <span>
        {skin}:{theme}:{resolvedTheme}
      </span>
      <button type="button" onClick={() => setTheme('light')}>
        Claro
      </button>
      <button type="button" onClick={() => setSkin('glass')}>
        Vidro
      </button>
      <button type="button" onClick={() => setReduceTransparency(true)}>
        Menos vidro
      </button>
    </div>
  )
}

const root = document.documentElement

describe('ThemeProvider', () => {
  beforeEach(() => {
    localStorage.clear()
    root.className = ''
    for (const key of ['skin', 'theme', 'transparency']) delete root.dataset[key]
    document.head.innerHTML = '<meta name="theme-color" content="#1e1f22">'
    vi.stubGlobal(
      'matchMedia',
      vi.fn().mockReturnValue({
        matches: false,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      }),
    )
  })

  it('keeps the default skin dark even when the mode asks for light', async () => {
    render(
      <ThemeProvider>
        <ThemeProbe />
      </ThemeProvider>,
    )
    expect(screen.getByText('default:system:dark')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Claro' }))

    await waitFor(() => expect(screen.getByText('default:light:dark')).toBeInTheDocument())
    expect(root).toHaveClass('dark')
    expect(root.dataset.skin).toBe('default')
    expect(localStorage.getItem('discordo-theme')).toBe('light')
  })

  it('applies and persists the glass skin in light mode, with its theme color', async () => {
    localStorage.setItem('discordo-theme', 'light')
    render(
      <ThemeProvider>
        <ThemeProbe />
      </ThemeProvider>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Vidro' }))

    await waitFor(() => expect(root.dataset.skin).toBe('glass'))
    expect(root.dataset.theme).toBe('light')
    expect(root).not.toHaveClass('dark')
    expect(document.querySelector('meta[name="theme-color"]')).toHaveAttribute('content', '#eef0f8')
    expect(localStorage.getItem('discordo-skin')).toBe('glass')
  })

  it('restores stored choices and marks reduced transparency', async () => {
    localStorage.setItem('discordo-skin', 'glass')
    render(
      <ThemeProvider>
        <ThemeProbe />
      </ThemeProvider>,
    )
    expect(screen.getByText('glass:system:light')).toBeInTheDocument()
    expect(root.dataset.transparency).toBeUndefined()

    fireEvent.click(screen.getByRole('button', { name: 'Menos vidro' }))

    await waitFor(() => expect(root.dataset.transparency).toBe('reduced'))
    expect(localStorage.getItem('discordo-reduce-transparency')).toBe('true')
  })

  it('falls back to defaults when storage is unavailable', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked')
    })
    render(
      <ThemeProvider>
        <ThemeProbe />
      </ThemeProvider>,
    )
    expect(screen.getByText('default:system:dark')).toBeInTheDocument()
    vi.restoreAllMocks()
  })
})

describe('resolveTheme', () => {
  it('follows the system only where the skin has both modes', () => {
    expect(resolveTheme('glass', 'system', true)).toBe('dark')
    expect(resolveTheme('glass', 'system', false)).toBe('light')
    expect(resolveTheme('default', 'light', false)).toBe('dark')
  })
})
