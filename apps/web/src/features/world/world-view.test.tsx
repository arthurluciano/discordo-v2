import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { TooltipProvider } from '@/components/ui/tooltip'
import { initialState, setState } from '@/lib/store'
import { WorldView } from './world-view'

describe('WorldView', () => {
  it('shows the map to someone outside the world and offers to enter it', () => {
    // jsdom has no canvas: the view must live without a context.
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null)
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise(() => {})),
    )

    const user = { id: 'u2', username: 'ana', display_name: 'Ana', avatar_url: '' }
    setState(() => ({
      ...initialState,
      status: 'ready',
      me: { id: 'u1', username: 'eu', display_name: 'Eu', avatar_url: '' },
      guilds: [
        {
          id: 'g',
          name: 'Guilda',
          icon_url: '',
          banner_url: '',
          owner_id: 'u1',
          permissions: -1,
          roles: [],
          channels: [
            { id: 'w', guild_id: 'g', type: 4, name: 'Mundo', topic: '', position: 2, overwrites: [], permissions: -1 },
          ],
          members: [{ user, nickname: '', roles: [], joined_at: '', online: true }],
        },
      ],
      voice: {
        u2: {
          user_id: 'u2',
          channel_id: 'w',
          guild_id: 'g',
          muted: false,
          deafened: false,
          tracks: [],
          x: 1200,
          y: 800,
        },
      },
    }))

    render(
      <QueryClientProvider client={new QueryClient()}>
        <TooltipProvider>
          <WorldView channelId="w" guildId="g" onBack={() => {}} />
        </TooltipProvider>
      </QueryClientProvider>,
    )

    expect(screen.getByRole('img', { name: 'Mapa do mundo' })).toBeInTheDocument()
    expect(screen.getByText('1 pessoa')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Entrar no mundo/ })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Ir para/ })).toBeNull()

    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })
})
