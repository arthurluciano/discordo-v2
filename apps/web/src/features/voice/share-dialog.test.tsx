import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ShareDialog } from './share-dialog'

const fakeTrack = () => ({
  kind: 'video',
  stop: vi.fn(),
  addEventListener: vi.fn(),
  removeEventListener: vi.fn(),
  applyConstraints: vi.fn(async () => {}),
  getSettings: () => ({ displaySurface: 'window' }),
})

afterEach(() => vi.unstubAllGlobals())

describe('ShareDialog', () => {
  it('opens the browser picker on the chosen kind of source, previews it, and drops it when closed', async () => {
    const track = fakeTrack()
    const getDisplayMedia = vi.fn(async () => ({ getVideoTracks: () => [track], getAudioTracks: () => [] }))
    vi.stubGlobal('navigator', { ...navigator, mediaDevices: { getDisplayMedia } })
    vi.stubGlobal('MediaStream', class {})
    vi.stubGlobal(
      'ResizeObserver',
      class {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    )

    const { rerender } = render(<ShareDialog open onOpenChange={() => {}} />)
    expect(screen.getByRole('button', { name: /Transmitir/ })).toBeDisabled()

    await userEvent.click(screen.getByRole('button', { name: /Janela/ }))

    expect(getDisplayMedia).toHaveBeenCalledWith(
      expect.objectContaining({ video: expect.objectContaining({ displaySurface: 'window' }) }),
    )
    expect(screen.getByText(/ninguém está vendo ainda/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Transmitir/ })).toBeEnabled()

    rerender(<ShareDialog open={false} onOpenChange={() => {}} />)
    expect(track.stop).toHaveBeenCalled()
  })
})
