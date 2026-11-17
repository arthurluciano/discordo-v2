import { useEffect, useState, type ComponentType } from 'react'
import { HeadphoneOff, Headphones, Mic, MicOff, MonitorUp, PhoneOff, Settings2, Video, VideoOff } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Toggle } from '@/components/ui/toggle'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { rememberEncoder } from '@/lib/media/codecs'
import { RESOLUTIONS } from '@/lib/media/presets'
import { encoderDetails, encoderLabel, encoderStats, type EncoderStats } from '@/lib/media/stats'
import { has, Permission } from '@/lib/permissions'
import { useStore } from '@/lib/store'
import { cn } from '@/lib/utils'
import { callPermissions, senderStats } from './call'
import { DeviceSettingsDialog } from './device-settings-dialog'
import { ShareDialog } from './share-dialog'
import { isMuted, useVoice, voice } from './voice-session'

type Icon = ComponentType<{ className?: string; 'aria-hidden'?: boolean }>

type ControlProps = {
  icon: Icon
  label: string
  pressed: boolean
  disabled?: boolean
  danger?: boolean
  onPressedChange: (pressed: boolean) => void
}

export const Control = ({
  icon: Icon,
  label,
  pressed,
  disabled = false,
  danger = false,
  onPressedChange,
}: ControlProps) => (
  <Tooltip>
    <TooltipTrigger asChild>
      <span>
        <Toggle
          variant="outline"
          pressed={pressed}
          disabled={disabled}
          onPressedChange={onPressedChange}
          aria-label={label}
          className={cn('size-10 p-0 max-md:size-11', danger && pressed && 'data-[state=on]:bg-media-muted')}
        >
          <Icon aria-hidden />
        </Toggle>
      </span>
    </TooltipTrigger>
    <TooltipContent>{label}</TooltipContent>
  </Tooltip>
)

const supportsScreenShare = () =>
  typeof navigator !== 'undefined' && typeof navigator.mediaDevices?.getDisplayMedia === 'function'

const ShareButton = () => {
  const { sharing, connection } = useVoice((s) => ({ sharing: s.screen !== null, connection: s.connection }))
  const [open, setOpen] = useState(false)

  return (
    <>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="outline"
            size="icon"
            aria-label={sharing ? 'Transmitindo a tela' : 'Compartilhar tela'}
            aria-haspopup="dialog"
            disabled={connection !== 'connected'}
            onClick={() => setOpen(true)}
            className={cn(
              'size-10 max-md:size-11',
              sharing && 'bg-media-screenshare text-white hover:bg-media-screenshare/80',
            )}
          >
            <MonitorUp aria-hidden />
          </Button>
        </TooltipTrigger>
        <TooltipContent>{sharing ? 'Transmitindo a tela' : 'Compartilhar tela'}</TooltipContent>
      </Tooltip>

      <ShareDialog open={open} onOpenChange={setOpen} />
    </>
  )
}

const STATS_INTERVAL_MS = 2_000

/** Polls the encoder of one of our videos while it is being sent. */
const useEncoderStats = (name: 'screen' | 'camera', active: boolean) => {
  const [stats, setStats] = useState<EncoderStats | null>(null)
  useEffect(() => {
    if (!active) return
    let previous: EncoderStats | null = null
    let stopped = false
    const poll = async () => {
      const report = await senderStats(name).catch(() => null)
      if (stopped || !report) return
      previous = encoderStats(report, previous)
      setStats(previous)
    }
    void poll()
    const timer = setInterval(() => void poll(), STATS_INTERVAL_MS)
    return () => {
      stopped = true
      clearInterval(timer)
      setStats(null)
    }
  }, [name, active])

  return active ? stats : null
}

/** "Codificação: GPU (…)" for each video we send; sent resolution, frame rate and bitrate on hover. */
export const EncoderInfo = ({ className }: { className?: string }) => {
  const v = useVoice((s) => ({
    screen: s.screen !== null,
    camera: s.camera !== null,
    resolution: s.prefs.screen.resolution,
    fps: s.prefs.screen.fps,
  }))
  const screen = useEncoderStats('screen', v.screen)
  const camera = useEncoderStats('camera', v.camera)
  const { resolution, fps } = v
  useEffect(() => {
    if (screen?.codec && screen.hardware !== null)
      rememberEncoder(screen.codec, { height: RESOLUTIONS[resolution].height, framerate: fps }, screen.hardware)
  }, [screen?.codec, screen?.hardware, resolution, fps])
  const rows = [['Tela', screen] as const, ['Câmera', camera] as const].flatMap(([what, s]) => (s ? [{ what, s }] : []))
  if (rows.length === 0) return null

  return (
    <div className={cn('flex min-w-0 flex-col gap-0.5 text-[11px] text-greyple', className)}>
      {rows.map(({ what, s }) => (
        <Tooltip key={what}>
          <TooltipTrigger asChild>
            <span tabIndex={0} className="truncate" data-numeric>
              {what} · {encoderLabel(s)}
            </span>
          </TooltipTrigger>
          <TooltipContent>
            {encoderLabel(s)}
            <br />
            {encoderDetails(s)}
          </TooltipContent>
        </Tooltip>
      ))}
    </div>
  )
}

type ControlsBarProps = {
  channelId: string
  /** False where the call has no camera video (the world). */
  camera?: boolean
  /** False where sharing is not allowed right now (the world, outside the cinema). */
  share?: boolean
}

/** Mute, deafen, camera, screen, devices, leave: shown on the stage of the call we are in. */
export const ControlsBar = ({ channelId, camera = true, share = true }: ControlsBarProps) => {
  const v = useVoice((s) => ({
    connection: s.connection,
    muted: isMuted(s),
    listenOnly: s.listenOnly,
    deafened: s.deafened,
    camera: s.camera,
  }))
  const perms = useStore((s) => callPermissions(s, channelId))
  const [settings, setSettings] = useState(false)
  const canStream = has(perms, Permission.Stream)
  const connected = v.connection === 'connected'

  return (
    <div className="flex flex-wrap items-center justify-center gap-2">
      <Control
        icon={v.muted ? MicOff : Mic}
        danger
        pressed={v.muted}
        disabled={v.listenOnly}
        label={v.listenOnly ? 'Você está só ouvindo' : v.muted ? 'Ativar microfone' : 'Mutar microfone'}
        onPressedChange={(m) => voice.setMuted(m)}
      />
      <Control
        icon={v.deafened ? HeadphoneOff : Headphones}
        danger
        pressed={v.deafened}
        label={v.deafened ? 'Ativar áudio' : 'Ensurdecer'}
        onPressedChange={(d) => voice.setDeafened(d)}
      />
      {canStream && camera ? (
        <Control
          icon={v.camera ? Video : VideoOff}
          pressed={v.camera !== null}
          disabled={!connected}
          label={v.camera ? 'Desligar câmera' : 'Ligar câmera'}
          onPressedChange={(on) => void voice.setCamera(on)}
        />
      ) : null}
      {canStream && share && supportsScreenShare() ? <ShareButton /> : null}
      <Control icon={Settings2} pressed={settings} label="Configurações de voz e vídeo" onPressedChange={setSettings} />
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="destructive"
            size="icon"
            className="size-10 max-md:size-11"
            aria-label="Sair da chamada"
            onClick={voice.leave}
          >
            <PhoneOff aria-hidden />
          </Button>
        </TooltipTrigger>
        <TooltipContent>Sair da chamada</TooltipContent>
      </Tooltip>
      <DeviceSettingsDialog open={settings} onOpenChange={setSettings} />
      <EncoderInfo className="basis-full items-center" />
    </div>
  )
}
