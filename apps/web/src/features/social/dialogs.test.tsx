import { describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { setState } from '@/lib/store'
import { RelationshipStatus } from '@/lib/wire'
import { FriendPicker } from './dialogs'

const bob = { id: 'b', username: 'bob', display_name: 'Bob', avatar_url: '' }

// The scroll area measures itself; jsdom has no ResizeObserver.
vi.stubGlobal(
  'ResizeObserver',
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  },
)

describe('FriendPicker', () => {
  it('submits the one friend picked as a list', async () => {
    setState((s) => ({ ...s, relationships: [{ user: bob, status: RelationshipStatus.Friend, online: true }] }))
    const onSubmit = vi.fn(async () => {})

    render(
      <QueryClientProvider client={new QueryClient()}>
        <FriendPicker
          open
          onOpenChange={() => {}}
          title="Novo grupo"
          description="Escolha"
          submitLabel="Criar"
          max={9}
          withName
          onSubmit={onSubmit}
        />
      </QueryClientProvider>,
    )

    await userEvent.click(screen.getByRole('checkbox'))
    await userEvent.type(screen.getByLabelText('Nome do grupo (opcional)'), ' Amigos ')
    await userEvent.click(screen.getByRole('button', { name: 'Criar' }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith(['b'], 'Amigos'))
  })
})
