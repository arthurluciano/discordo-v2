import { useEffect, useState, type ComponentType } from 'react'
import { HeadphoneOff, Headphones, Mic, MicOff, MonitorUp, PhoneOff, Settings2, Video, VideoOff } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuRadioGroup, DropdownMenuRadioItem,
  DropdownMenuSeparator, DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Field, FieldContent, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Switch } from '@/components/ui/switch'
import { Toggle } from '@/components/ui/toggle'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { CODECS, hardwareCodecs, sendableCodecs, type CodecChoice } from '@/lib/media/codecs'
import { RESOLUTIONS, screenEncoding, type Resolution } from '@/lib/media/presets'
import { encoderDetails, encoderLabel, encoderStats, type EncoderStats } from '@/lib/media/stats'
import { has, Permission } from '@/lib/permissions'
import { useStore } from '@/lib/store'
import { cn } from '@/lib/utils'
import { callPermissions, senderStats } from './call'
import { useVoice } from './voice-session'

type Icon = ComponentType<{ className?: string; 'aria-hidden'?: boolean }>

type ControlProps = { icon: Icon; label: string; pressed: boolean; disabled?: boolean; danger?: boolean; onPressedChange: (pressed: boolean) => void }

export const Control = ({ icon: Icon, label, pressed, disabled = false, danger = false, onPressedChange }: ControlProps) => (
  <Tooltip>
    <TooltipTrigger asChild>
      <span>
        <Toggle variant="outline" pressed={pressed} disabled={disabled} onPressedChange={onPressedChange} aria-label={label}
          className={cn('size-10 p-0 max-md:size-11', danger && pressed && 'data-[state=on]:bg-media-muted')}>
          <Icon aria-hidden />
        </Toggle>
      </span>
    </TooltipTrigger>
    <TooltipContent>{label}</TooltipContent>
  </Tooltip>
)

const supportsScreenShare = () => typeof navigator !== 'undefined' && typeof navigator.mediaDevices?.getDisplayMedia === 'function'

const ShareMenu = () => {
  const v = useVoice()
  const preset = v.prefs.screen
  const sharing = v.screen !== null
  const [hardware, setHardware] = useState<ReadonlySet<string> | null>(null)
  // Asked each time the menu opens, for the preset in use: GPU support depends on resolution and frame rate.
  const probe = (open: boolean) => {
    if (!open) return
    const { width, height } = RESOLUTIONS[preset.resolution]
    void hardwareCodecs({ width, height, framerate: preset.fps, bitrate: screenEncoding(preset).maxBitrate }).then(setHardware)
  }
  const codecLabel = (id: keyof typeof CODECS) =>
    `${CODECS[id]}${hardware === null ? '' : hardware.has(`video/${id}`) ? ' (GPU)' : ' (CPU)'}`
  return (
    <DropdownMenu onOpenChange={probe}>
      <Tooltip>
        <TooltipTrigger asChild>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="icon" aria-label="Compartilhar tela" disabled={v.connection !== 'connected'}
              className={cn('size-10 max-md:size-11', sharing && 'bg-media-screenshare text-white hover:bg-media-screenshare/80')}>
              <MonitorUp aria-hidden />
            </Button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent>{sharing ? 'Compartilhando tela' : 'Compartilhar tela'}</TooltipContent>
      </Tooltip>
      <DropdownMenuContent side="top" className="w-56">
        <DropdownMenuLabel>Resolução</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={preset.resolution} onValueChange={(r) => void v.setScreenPreset({ ...preset, resolution: r as Resolution })}>
          {Object.entries(RESOLUTIONS).map(([id, { label }]) => (
            <DropdownMenuRadioItem key={id} value={id} onSelect={(e) => e.preventDefault()}>{label}</DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
        <DropdownMenuSeparator />
        <DropdownMenuLabel>Quadros por segundo</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={String(preset.fps)} onValueChange={(fps) => void v.setScreenPreset({ ...preset, fps: fps === '60' ? 60 : 30 })}>
          <DropdownMenuRadioItem value="30" onSelect={(e) => e.preventDefault()}>30 fps (texto nítido)</DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="60" onSelect={(e) => e.preventDefault()}>60 fps (movimento)</DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
        <DropdownMenuSeparator />
        <DropdownMenuLabel>Codec</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={preset.codec ?? 'auto'} onValueChange={(codec) => void v.setScreenPreset({ ...preset, codec: codec as CodecChoice })}>
          <DropdownMenuRadioItem value="auto" onSelect={(e) => e.preventDefault()}>Automático (GPU primeiro)</DropdownMenuRadioItem>
          {sendableCodecs().map((id) => (
            <DropdownMenuRadioItem key={id} value={id} onSelect={(e) => e.preventDefault()}>{codecLabel(id)}</DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
        <DropdownMenuSeparator />
        {sharing ? (
          <>
            <DropdownMenuItem onSelect={() => void v.switchScreen()}>Trocar tela</DropdownMenuItem>
            <DropdownMenuItem variant="destructive" onSelect={() => void v.stopScreen()}>Parar compartilhamento</DropdownMenuItem>
          </>
        ) : (
          <DropdownMenuItem onSelect={() => void v.startScreen()}>Compartilhar tela</DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
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
  const v = useVoice()
  const screen = useEncoderStats('screen', v.screen !== null)
  const camera = useEncoderStats('camera', v.camera !== null)
  const rows = [['Tela', screen] as const, ['Câmera', camera] as const].flatMap(([what, s]) => (s ? [{ what, s }] : []))
  if (rows.length === 0) return null
  return (
    <div className={cn('flex min-w-0 flex-col gap-0.5 text-[11px] text-greyple', className)}>
      {rows.map(({ what, s }) => (
        <Tooltip key={what}>
          <TooltipTrigger asChild>
            <span tabIndex={0} className="truncate" data-numeric>{what} · {encoderLabel(s)}</span>
          </TooltipTrigger>
          <TooltipContent>{encoderLabel(s)}<br />{encoderDetails(s)}</TooltipContent>
        </Tooltip>
      ))}
    </div>
  )
}

const useDevices = (open: boolean) => {
  const [devices, setDevices] = useState<MediaDeviceInfo[]>([])
  useEffect(() => {
    const md = navigator.mediaDevices
    if (!open || !md?.enumerateDevices) return
    const load = () => void md.enumerateDevices().then(setDevices).catch(() => {})
    load()
    md.addEventListener('devicechange', load)
    return () => md.removeEventListener('devicechange', load)
  }, [open])
  return devices
}

const DeviceSelect = ({ label, kind, devices, value, onChange }: { label: string; kind: MediaDeviceKind; devices: MediaDeviceInfo[]; value: string | null; onChange: (id: string) => void }) => {
  // Without a granted permission the browser lists devices with empty IDs.
  const options = devices.filter((d) => d.kind === kind && d.deviceId)
  return (
    <Field>
      <FieldLabel>{label}</FieldLabel>
      <Select {...(value && options.some((d) => d.deviceId === value) ? { value } : {})} onValueChange={onChange} disabled={options.length === 0}>
        <SelectTrigger className="w-full"><SelectValue placeholder="Padrão do sistema" /></SelectTrigger>
        <SelectContent position="popper">
          {options.map((d) => <SelectItem key={d.deviceId} value={d.deviceId}>{d.label || label}</SelectItem>)}
        </SelectContent>
      </Select>
    </Field>
  )
}

const supportsOutputChoice = () => typeof HTMLMediaElement !== 'undefined' && 'setSinkId' in HTMLMediaElement.prototype

export const DeviceSettings = ({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) => {
  const v = useVoice()
  const devices = useDevices(open)
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-[min(94vw,28rem)] gap-0 bg-surface-sidebar p-0 text-snow sm:max-w-[28rem]">
        <SheetHeader className="border-b border-border bg-surface-content pr-14">
          <SheetTitle className="text-xl font-extrabold">Voz e vídeo</SheetTitle>
          <SheetDescription>Dispositivos e processamento do microfone.</SheetDescription>
        </SheetHeader>
        <div className="grid gap-5 overflow-y-auto p-4 sm:p-5">
          <DeviceSelect label="Microfone" kind="audioinput" devices={devices} value={v.prefs.input} onChange={(id) => void v.setDevice('input', id)} />
          {supportsOutputChoice() ? (
            <DeviceSelect label="Saída de áudio" kind="audiooutput" devices={devices} value={v.prefs.output} onChange={(id) => void v.setDevice('output', id)} />
          ) : <p className="text-xs text-muted-foreground">Este navegador não permite escolher a saída de áudio.</p>}
          <DeviceSelect label="Câmera" kind="videoinput" devices={devices} value={v.prefs.camera} onChange={(id) => void v.setDevice('camera', id)} />
          <Field orientation="horizontal">
            <FieldContent>
              <FieldLabel htmlFor="voice-noise-gate">Porta de ruído</FieldLabel>
              <FieldDescription>Fecha o microfone entre as frases, quando só o ventilador e o teclado estariam saindo.</FieldDescription>
            </FieldContent>
            <Switch id="voice-noise-gate" checked={v.prefs.noiseGate} onCheckedChange={v.setNoiseGate} />
          </Field>
        </div>
      </SheetContent>
    </Sheet>
  )
}

/** Mute, deafen, camera, screen, devices, leave: shown on the stage of the call we are in. */
export const ControlsBar = ({ channelId }: { channelId: string }) => {
  const v = useVoice()
  const perms = useStore((s) => callPermissions(s, channelId))
  const [settings, setSettings] = useState(false)
  const canStream = has(perms, Permission.Stream)
  const connected = v.connection === 'connected'

  return (
    <div className="flex flex-wrap items-center justify-center gap-2">
      <Control icon={v.muted ? MicOff : Mic} danger pressed={v.muted} disabled={v.listenOnly}
        label={v.listenOnly ? 'Você está só ouvindo' : v.muted ? 'Ativar microfone' : 'Mutar microfone'} onPressedChange={(m) => v.setMuted(m)} />
      <Control icon={v.deafened ? HeadphoneOff : Headphones} danger pressed={v.deafened}
        label={v.deafened ? 'Ativar áudio' : 'Ensurdecer'} onPressedChange={(d) => v.setDeafened(d)} />
      {canStream ? (
        <Control icon={v.camera ? Video : VideoOff} pressed={v.camera !== null} disabled={!connected}
          label={v.camera ? 'Desligar câmera' : 'Ligar câmera'} onPressedChange={(on) => void v.setCamera(on)} />
      ) : null}
      {canStream && supportsScreenShare() ? <ShareMenu /> : null}
      <Control icon={Settings2} pressed={settings} label="Configurações de voz e vídeo" onPressedChange={setSettings} />
      <Tooltip>
        <TooltipTrigger asChild>
          <Button variant="destructive" size="icon" className="size-10 max-md:size-11" aria-label="Sair da chamada" onClick={v.leave}><PhoneOff aria-hidden /></Button>
        </TooltipTrigger>
        <TooltipContent>Sair da chamada</TooltipContent>
      </Tooltip>
      <DeviceSettings open={settings} onOpenChange={setSettings} />
      <EncoderInfo className="basis-full items-center" />
    </div>
  )
}
