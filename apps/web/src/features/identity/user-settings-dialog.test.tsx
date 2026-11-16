import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { setState } from '@/lib/store'
import { UserSettingsDialog } from './user-settings-dialog'

const me = { id: 'a', username: 'alice', display_name: 'Alice', avatar_url: '' }

afterEach(() => vi.restoreAllMocks())

describe('UserSettingsDialog', () => {
  it('previews the profile while editing and saves only through the button', async () => {
    setState((s) => ({ ...s, me }))
    vi.spyOn(api, 'profile').mockResolvedValue({
      ...me,
      bio: '',
      pronouns: '',
      banner_color: 0,
      created_at: '2026-01-02T00:00:00Z',
      mutual_guild_ids: [],
    })
    const update = vi.spyOn(api, 'updateProfile').mockResolvedValue(me)

    render(
      <QueryClientProvider client={new QueryClient()}>
        <UserSettingsDialog open section="profile" onSectionChange={() => {}} onOpenChange={() => {}} />
      </QueryClientProvider>,
    )

    const bio = await screen.findByLabelText('Sobre mim')
    expect(screen.getByRole('button', { name: /Salvar/ })).toBeDisabled()

    await userEvent.type(bio, 'Faz bolos.')
    await userEvent.type(screen.getByLabelText('Pronomes'), 'ela/dela')
    await userEvent.click(screen.getByRole('radio', { name: '#eb459e' }))

    // The preview card follows the form before anything is saved.
    expect(screen.getByText('Faz bolos.')).toBeInTheDocument()
    expect(screen.getByText(/ela\/dela/)).toBeInTheDocument()
    expect(update).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: /Salvar/ }))

    await waitFor(() =>
      expect(update).toHaveBeenCalledWith(
        expect.objectContaining({ bio: 'Faz bolos.', pronouns: 'ela/dela', banner_color: 0xeb459e }),
      ),
    )
  })
})
