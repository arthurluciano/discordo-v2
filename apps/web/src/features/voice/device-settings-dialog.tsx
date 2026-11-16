// Voice and video settings: microphone (with its level), audio output, noise gate, camera (with its preview).
// The previews use the call's own tracks: opening this never turns the camera or the microphone on.
import { useEffect, useState, type ReactNode } from 'react'
import { motion } from 'motion/react'
import { Mic, Video } from 'lucide-react'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldContent, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { TrackPreview } from './track-preview'
import { useAudioLevel } from './use-audio-level'
import { useVoice, voice } from './voice-session'

const useDevices = (open: boolean) => {
  const [devices, setDevices] = useState<MediaDeviceInfo[]>([])

  useEffect(() => {
    const md = navigator.mediaDevices
    if (!open || !md?.enumerateDevices) return

    const load = () =>
      void md
        .enumerateDevices()
        .then(setDevices)
        .catch(() => {})

    load()
    md.addEventListener('devicechange', load)

    return () => md.removeEventListener('devicechange', load)
  }, [open])

  return devices
}

const DeviceSelect = ({
  label,
  kind,
  devices,
  value,
  onChange,
}: {
  label: string
  kind: MediaDeviceKind
  devices: MediaDeviceInfo[]
  value: string | null
  onChange: (id: string) => void
}) => {
  // Without a granted permission the browser lists devices with empty IDs.
  const options = devices.filter((d) => d.kind === kind && d.deviceId)

  return (
    <Field>
      <FieldLabel className="section-label">{label}</FieldLabel>

      <Select
        {...(value && options.some((d) => d.deviceId === value) ? { value } : {})}
        onValueChange={onChange}
        disabled={options.length === 0}
      >
        <SelectTrigger className="w-full">
          <SelectValue placeholder="Padrão do sistema" />
        </SelectTrigger>

        <SelectContent position="popper">
          {options.map((d) => (
            <SelectItem key={d.deviceId} value={d.deviceId}>
              {d.label || label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </Field>
  )
}

/** How loud the microphone is, as the others hear it (after the noise gate). */
const MicLevel = ({ track }: { track: MediaStreamTrack | null }) => {
  const level = useAudioLevel(track)

  return (
    <div className="space-y-1.5">
      <div className="h-2 overflow-hidden rounded-full bg-surface-active" aria-hidden>
        <motion.div className="h-full origin-left rounded-full bg-spring-green" style={{ scaleX: level }} />
      </div>

      <p className="text-xs text-greyple">
        {track
          ? 'Fale para testar: a barra mostra o que os outros ouvem.'
          : 'Entre numa chamada para testar o microfone.'}
      </p>
    </div>
  )
}

const Section = ({ icon, title, children }: { icon: ReactNode; title: string; children: ReactNode }) => (
  <section className="space-y-5 rounded-xl border border-border bg-surface-raised p-4 sm:p-5">
    <h3 className="flex items-center gap-2 text-base font-bold text-snow">
      {icon}
      {title}
    </h3>

    {children}
  </section>
)

const supportsOutputChoice = () => typeof HTMLMediaElement !== 'undefined' && 'setSinkId' in HTMLMediaElement.prototype

export const DeviceSettingsDialog = ({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) => {
  const { prefs, mic, camera } = useVoice((s) => ({ prefs: s.prefs, mic: s.mic, camera: s.camera }))
  const devices = useDevices(open)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[92vh] overflow-y-auto bg-surface-content sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle className="text-xl font-extrabold">Voz e vídeo</DialogTitle>
          <DialogDescription>
            Dispositivos e processamento do microfone. Tudo vale na hora, inclusive na chamada.
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-4 md:grid-cols-2">
          <Section icon={<Mic className="size-4 text-blurple-soft" aria-hidden />} title="Voz">
            <DeviceSelect
              label="Microfone"
              kind="audioinput"
              devices={devices}
              value={prefs.input}
              onChange={(id) => void voice.setDevice('input', id)}
            />

            <MicLevel track={mic} />

            {supportsOutputChoice() ? (
              <DeviceSelect
                label="Saída de áudio"
                kind="audiooutput"
                devices={devices}
                value={prefs.output}
                onChange={(id) => void voice.setDevice('output', id)}
              />
            ) : (
              <p className="text-xs text-muted-foreground">Este navegador não permite escolher a saída de áudio.</p>
            )}

            <Field orientation="horizontal">
              <FieldContent>
                <FieldLabel htmlFor="voice-noise-gate">Porta de ruído</FieldLabel>
                <FieldDescription>
                  Fecha o microfone entre as frases, quando só o ventilador e o teclado estariam saindo.
                </FieldDescription>
              </FieldContent>

              <Switch id="voice-noise-gate" checked={prefs.noiseGate} onCheckedChange={voice.setNoiseGate} />
            </Field>
          </Section>

          <Section icon={<Video className="size-4 text-blurple-soft" aria-hidden />} title="Vídeo">
            <DeviceSelect
              label="Câmera"
              kind="videoinput"
              devices={devices}
              value={prefs.camera}
              onChange={(id) => void voice.setDevice('camera', id)}
            />

            <div className="aspect-video overflow-hidden rounded-lg border border-border bg-black">
              <TrackPreview
                track={camera}
                placeholder={
                  <>
                    <Video className="size-8" aria-hidden />
                    Ligue a câmera na chamada para ver a prévia aqui.
                  </>
                }
              />
            </div>
          </Section>
        </div>
      </DialogContent>
    </Dialog>
  )
}
