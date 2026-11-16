import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { TooltipProvider } from '@/components/ui/tooltip'
import { initialState, setState } from '@/lib/store'
import { VoiceDock } from './voice-dock'
import { VoiceStage } from './voice-stage'

describe('VoiceStage', () => {
  it('shows who is in the call and offers to join it', () => {
    const user = { id: 'u2', username: 'ana', display_name: 'Ana', avatar_url: '' }
    setState(() => ({
      ...initialState, status: 'ready', me: { id: 'u1', username: 'eu', display_name: 'Eu', avatar_url: '' },
      dms: [{ id: 'dm1', type: 2, name: '', recipients: [user] }],
      voice: { u2: { user_id: 'u2', channel_id: 'dm1', muted: true, deafened: false, tracks: ['mic', 'screen'] } },
    }))
    render(<TooltipProvider><VoiceStage channelId="dm1" onBack={() => {}} /><VoiceDock /></TooltipProvider>)
    expect(screen.getByRole('heading', { name: 'Ana' })).toBeInTheDocument()
    expect(screen.getByText('1 pessoa')).toBeInTheDocument()
    expect(screen.getByLabelText('Ana, compartilhando tela')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Entrar na chamada/ })).toBeInTheDocument()
    expect(screen.queryByLabelText('Chamada')).toBeNull() // the dock only shows during a call
  })
})
