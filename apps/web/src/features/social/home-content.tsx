import { useEffect, useRef, useState } from 'react'
import { LogOut, Phone, PhoneOff, UserPlus } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { ErrorBoundary } from '@/design/error-boundary'
import { ChannelChat } from '@/features/chat/channel-chat'
import { useMe } from '@/features/identity/users'
import { useVoice, voice } from '@/features/voice/voice-session'
import { VoiceStage } from '@/features/voice/voice-stage'
import { api } from '@/lib/api'
import { useCallers, useStore } from '@/lib/store'
import { ChannelType, type DM } from '@/lib/wire'
import { navigate } from '../shell/navigation'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { FriendPicker } from './dialogs'
import { dmTitle } from './dm'
import { FriendsView } from './friends-view'

// The friends screen when dmId is null, else the DM: its chat, a call button, and the stage while in its call.
type HomeContentProps = { dmId: string | null; onBack: () => void }

const GROUP_MAX = 10

const DMView = ({ dm, onBack }: { dm: DM; onBack: () => void }) => {
  const me = useMe()
  const inCall = useVoice((s) => s.channelId === dm.id)
  const callers = useCallers(dm.id).length
  const [adding, setAdding] = useState(false)
  const [leaving, setLeaving] = useState(false)
  const title = dmTitle(dm, me?.id)
  const group = dm.type === ChannelType.Group

  const actions = (
    <>
      {inCall ? (
        <Button variant="ghost" size="icon-sm" onClick={voice.leave} aria-label="Sair da chamada" title="Sair da chamada"><PhoneOff className="text-destructive" aria-hidden /></Button>
      ) : (
        <Button variant="ghost" size="icon-sm" onClick={() => voice.join(dm.id)} aria-label="Iniciar chamada" title="Iniciar chamada"><Phone aria-hidden /></Button>
      )}
      {group ? (
        <>
          <Button variant="ghost" size="icon-sm" onClick={() => setAdding(true)} disabled={dm.recipients.length >= GROUP_MAX} aria-label="Adicionar membro" title="Adicionar membro"><UserPlus aria-hidden /></Button>
          <Button variant="ghost" size="icon-sm" onClick={() => setLeaving(true)} aria-label="Sair do grupo" title="Sair do grupo"><LogOut aria-hidden /></Button>
        </>
      ) : null}
    </>
  )

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      {inCall ? (
        <div className="flex h-1/2 min-h-0 shrink-0 border-b border-white/5">
          <ErrorBoundary label="Chamada"><VoiceStage channelId={dm.id} onBack={onBack} /></ErrorBoundary>
        </div>
      ) : callers > 0 ? (
        <div className="flex shrink-0 items-center gap-3 border-b border-white/5 bg-spring-green/10 px-4 py-2 text-sm">
          <Phone className="size-4 text-spring-green" aria-hidden />
          <span className="flex-1 text-fog">{callers === 1 ? '1 pessoa na chamada' : `${callers} pessoas na chamada`}</span>
          <Button size="sm" onClick={() => voice.join(dm.id)}>Entrar na chamada</Button>
        </div>
      ) : null}
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
        <ErrorBoundary label="Conversa"><ChannelChat channelId={dm.id} title={title} actions={actions} onBack={onBack} /></ErrorBoundary>
      </div>

      {group ? (
        <>
          <FriendPicker
            open={adding}
            onOpenChange={setAdding}
            title="Adicionar membro"
            description={`Adicione amigos a ${title}.`}
            submitLabel="Adicionar"
            exclude={dm.recipients.map((r) => r.id)}
            max={GROUP_MAX - dm.recipients.length}
            onSubmit={(ids) => api.addToGroup(dm.id, ids)}
          />
          <ConfirmDialog
            open={leaving}
            onOpenChange={setLeaving}
            title={`Sair de ${title}?`}
            description="Você deixa de ver as mensagens do grupo até alguém adicionar você de novo."
            confirmLabel="Sair do grupo"
            onConfirm={async () => {
              await api.leaveGroup(dm.id)
              navigate.home()
            }}
          />
        </>
      ) : null}
    </div>
  )
}

const DMScreen = ({ dmId, onBack }: { dmId: string; onBack: () => void }) => {
  const dm = useStore((s) => s.dms.find((d) => d.id === dmId))
  // Opening a DM navigates before its DM_CREATE arrives, so only a DM that was here and left counts as gone.
  const seen = useRef(false)
  useEffect(() => {
    if (dm) seen.current = true
    else if (seen.current) navigate.home()
  }, [dm])

  if (!dm) return <div className="grid flex-1 place-items-center"><Spinner aria-label="Carregando conversa" /></div>
  return <DMView dm={dm} onBack={onBack} />
}

export const HomeContent = ({ dmId, onBack }: HomeContentProps) =>
  dmId ? <DMScreen key={dmId} dmId={dmId} onBack={onBack} /> : <FriendsView onBack={onBack} />
