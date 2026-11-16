// CONTRACT (implemented by the voice area): call status and controls shown above the user panel
// while in a call (renders nothing otherwise).
import type { ReactNode } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import { HeadphoneOff, Headphones, Mic, MicOff, PhoneOff, RotateCw } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { SPRING_SOFT, useTransition } from '@/design/motion'
import { displayName } from '@/features/identity/users'
import { useStore } from '@/lib/store'
import { cn } from '@/lib/utils'
import { Control, EncoderInfo } from './voice-controls'
import { isMuted, useVoice, voice } from './voice-session'

/** "channel / guild" for a voice channel, the DM's name (or its people) for a DM. */
export const useCallTitle = (channelId: string | null) => useStore((s) => {
  if (!channelId) return ''
  for (const g of s.guilds) {
    const c = g.channels.find((x) => x.id === channelId)
    if (c) return `${c.name} / ${g.name}`
  }
  const dm = s.dms.find((d) => d.id === channelId)
  if (!dm) return 'Chamada'
  return dm.name || dm.recipients.filter((u) => u.id !== s.me?.id).map((u) => displayName(u)).join(', ') || 'Chamada'
})

const STATUS = {
  idle: { text: '', className: '' },
  connecting: { text: 'Conectando…', className: 'text-media-connecting' },
  connected: { text: 'Voz conectada', className: 'text-spring-green' },
  failed: { text: 'Falha na chamada', className: 'text-ekko-red' },
} as const

const IconButton = ({ label, onClick, variant = 'ghost', children }: { label: string; onClick: () => void; variant?: 'ghost' | 'destructive'; children: ReactNode }) => (
  <Tooltip>
    <TooltipTrigger asChild>
      <Button variant={variant} size="icon-sm" onClick={onClick} aria-label={label}>{children}</Button>
    </TooltipTrigger>
    <TooltipContent>{label}</TooltipContent>
  </Tooltip>
)

export const VoiceDock = () => {
  const v = useVoice((s) => ({ channelId: s.channelId, connection: s.connection, muted: isMuted(s), listenOnly: s.listenOnly, deafened: s.deafened, error: s.error }))
  const title = useCallTitle(v.channelId)
  const transition = useTransition(SPRING_SOFT)
  const status = STATUS[v.connection]

  return (
    <AnimatePresence initial={false}>
      {v.channelId ? (
        <motion.section key="voice-dock" aria-label="Chamada" initial={{ height: 0, opacity: 0 }} animate={{ height: 'auto', opacity: 1 }} exit={{ height: 0, opacity: 0 }} transition={transition} className="overflow-hidden border-b border-white/5">
          <div className="flex items-center gap-2 p-3">
            <span className="min-w-0 flex-1 leading-tight">
              <span className={cn('flex items-center gap-1.5 text-xs font-bold', status.className)}>
                {v.connection === 'connecting' ? <Spinner aria-hidden className="size-3" /> : <span className="size-2 rounded-full bg-current" />}
                {status.text}
              </span>
              <span className="mt-1 block truncate text-[11px] text-greyple">{title}</span>
            </span>
            {v.connection === 'failed' ? (
              <IconButton label="Tentar de novo" onClick={() => v.channelId && voice.join(v.channelId)}><RotateCw aria-hidden /></IconButton>
            ) : (
              <>
                <Control icon={v.muted ? MicOff : Mic} danger pressed={v.muted} disabled={v.listenOnly}
                  label={v.listenOnly ? 'Você está só ouvindo' : v.muted ? 'Ativar microfone' : 'Mutar microfone'} onPressedChange={(m) => voice.setMuted(m)} />
                <Control icon={v.deafened ? HeadphoneOff : Headphones} danger pressed={v.deafened}
                  label={v.deafened ? 'Ativar áudio' : 'Ensurdecer'} onPressedChange={(d) => voice.setDeafened(d)} />
              </>
            )}
            <IconButton label="Sair da chamada" variant="destructive" onClick={voice.leave}><PhoneOff aria-hidden /></IconButton>
          </div>
          <EncoderInfo className="px-3 pb-2" />
          {v.error ? <p role="alert" className="px-3 pb-3 text-xs text-ekko-red">{v.error}</p> : null}
        </motion.section>
      ) : null}
    </AnimatePresence>
  )
}
