// The screen share dialog: pick what to share (a whole screen, a window or a tab), see it live, tune the quality, go live.
// The page cannot list the computer's screens and windows (only the browser can), so each card opens the browser's
// picker already on that kind of source, and what was picked shows here before anyone sees it.
import { useEffect, useRef, useState, type ComponentType } from 'react'
import { AppWindow, Globe, Monitor, MonitorUp, Radio, RefreshCw, Square } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldLabel } from '@/components/ui/field'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Toggle } from '@/components/ui/toggle'
import { captureError, captureScreen, type ScreenSource, type ScreenSurface } from '@/lib/media/capture'
import { CODECS, hardwareCodecs, sendableCodecs, type CodecChoice } from '@/lib/media/codecs'
import { RESOLUTIONS, screenConstraints, screenEncoding, type Resolution, type ScreenPreset } from '@/lib/media/presets'
import { cn } from '@/lib/utils'
import { useVoice, voice } from './voice-session'

const SURFACES: {
  surface: ScreenSurface
  label: string
  description: string
  icon: ComponentType<{ className?: string }>
}[] = [
  { surface: 'monitor', label: 'Tela inteira', description: 'Tudo o que aparece num monitor.', icon: Monitor },
  { surface: 'window', label: 'Janela', description: 'Um aplicativo, mesmo atrás de outros.', icon: AppWindow },
  { surface: 'browser', label: 'Aba do navegador', description: 'Uma aba, com o som dela.', icon: Globe },
]

const isCancel = (cause: unknown) => cause instanceof DOMException && cause.name === 'NotAllowedError'

/** Which codecs the GPU encodes at this quality (asked again when it changes: support depends on it). */
const useHardwareCodecs = (preset: ScreenPreset, open: boolean) => {
  const [hardware, setHardware] = useState<ReadonlySet<string> | null>(null)

  useEffect(() => {
    if (!open) return

    let stale = false
    const { width, height } = RESOLUTIONS[preset.resolution]
    const probe = { width, height, framerate: preset.fps, bitrate: screenEncoding(preset).maxBitrate }

    void hardwareCodecs(probe).then((found) => {
      if (!stale) setHardware(found)
    })

    return () => {
      stale = true
    }
  }, [preset, open])

  return hardware
}

const Preview = ({ track }: { track: MediaStreamTrack | null }) => {
  const ref = useRef<HTMLVideoElement>(null)

  useEffect(() => {
    const el = ref.current
    if (!el || !track) return

    el.srcObject = new MediaStream([track])

    return () => {
      el.srcObject = null
    }
  }, [track])

  if (!track) {
    return (
      <div className="grid size-full place-items-center text-center text-sm text-greyple">
        <span className="flex flex-col items-center gap-2">
          <MonitorUp className="size-8" aria-hidden />
          Escolha acima o que compartilhar. A prévia aparece aqui antes de alguém ver.
        </span>
      </div>
    )
  }

  return <video ref={ref} autoPlay muted playsInline className="size-full object-contain" />
}

const QualitySettings = ({ preset, hardware }: { preset: ScreenPreset; hardware: ReadonlySet<string> | null }) => {
  const audio = useVoice((s) => s.prefs.screenAudio)
  const set = (patch: Partial<ScreenPreset>) => void voice.setScreenPreset({ ...preset, ...patch })

  const codecLabel = (id: keyof typeof CODECS) =>
    `${CODECS[id]}${hardware === null ? '' : hardware.has(`video/${id}`) ? ' (GPU)' : ' (CPU)'}`

  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      <Field>
        <FieldLabel className="section-label">Resolução</FieldLabel>
        <Select value={preset.resolution} onValueChange={(resolution) => set({ resolution: resolution as Resolution })}>
          <SelectTrigger className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent position="popper">
            {Object.entries(RESOLUTIONS).map(([id, { label }]) => (
              <SelectItem key={id} value={id}>
                {label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>

      <Field>
        <FieldLabel className="section-label">Quadros por segundo</FieldLabel>
        <div className="grid grid-cols-2 gap-1" role="radiogroup" aria-label="Quadros por segundo">
          {([30, 60] as const).map((fps) => (
            <Toggle
              key={fps}
              role="radio"
              aria-checked={preset.fps === fps}
              pressed={preset.fps === fps}
              onPressedChange={() => set({ fps })}
              title={fps === 30 ? 'Texto nítido' : 'Movimento suave'}
            >
              {fps} fps
            </Toggle>
          ))}
        </div>
      </Field>

      <Field>
        <FieldLabel className="section-label">Codec</FieldLabel>
        <Select value={preset.codec ?? 'auto'} onValueChange={(codec) => set({ codec: codec as CodecChoice })}>
          <SelectTrigger className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent position="popper">
            <SelectItem value="auto">Automático (GPU primeiro)</SelectItem>
            {sendableCodecs().map((id) => (
              <SelectItem key={id} value={id}>
                {codecLabel(id)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>

      <Field orientation="horizontal" className="self-end">
        <Switch id="share-audio" checked={audio} onCheckedChange={voice.setScreenAudio} />
        <FieldLabel htmlFor="share-audio">Incluir o áudio</FieldLabel>
      </Field>
    </div>
  )
}

export const ShareDialog = ({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) => {
  const { live, preset, audio } = useVoice((s) => ({
    live: s.screen,
    preset: s.prefs.screen,
    audio: s.prefs.screenAudio,
  }))
  const hardware = useHardwareCodecs(preset, open)

  // Picked but not shared yet: ours to stop until it goes live.
  const [preview, setPreview] = useState<ScreenSource | null>(null)
  const [picking, setPicking] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const previewRef = useRef(preview)
  previewRef.current = preview

  const dropPreview = () => {
    previewRef.current?.video.stop()
    previewRef.current?.audio?.stop()
  }

  // Closing the dialog drops a preview that never went live.
  useEffect(() => {
    if (open) return

    dropPreview()
    setPreview(null)
    setError(null)
  }, [open])

  // So does leaving the call with the dialog still open (the controls unmount).
  useEffect(() => dropPreview, [])

  // The browser's own "stop sharing" bar ends a preview too.
  useEffect(() => {
    if (!preview) return

    const ended = () => setPreview(null)
    preview.video.addEventListener('ended', ended)

    return () => preview.video.removeEventListener('ended', ended)
  }, [preview])

  // A quality change while previewing: what is shown follows it.
  useEffect(() => {
    void preview?.video.applyConstraints(screenConstraints(preset)).catch(() => {})
  }, [preview, preset])

  const pick = async (surface: ScreenSurface) => {
    setPicking(true)
    setError(null)

    try {
      const source = await captureScreen(preset, { surface, audio })

      if (live) {
        // Already live: the browser's picker was the confirmation, so the new source replaces the old at once.
        await voice.switchScreen(source)
        return
      }

      dropPreview()
      setPreview(source)
    } catch (cause) {
      if (!isCancel(cause)) setError(captureError(cause, 'screen'))
    } finally {
      setPicking(false)
    }
  }

  const goLive = () => {
    if (!preview) return

    previewRef.current = null // the call owns the tracks from here on
    setPreview(null)
    void voice.startScreen(preview)
    onOpenChange(false)
  }

  const stop = () => {
    void voice.stopScreen()
    onOpenChange(false)
  }

  const shown = preview?.video ?? live

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[92vh] overflow-y-auto bg-surface-content sm:max-w-5xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-xl font-extrabold">
            <MonitorUp className="size-5 text-blurple-soft" aria-hidden />
            {live ? 'Transmitindo a tela' : 'Compartilhar tela'}
          </DialogTitle>
          <DialogDescription>
            {live
              ? 'Troque a fonte ou ajuste a qualidade: quem assiste vê a mudança na hora.'
              : 'Escolha o que mostrar, confira a prévia e comece quando estiver pronto.'}
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-3 sm:grid-cols-3" role="group" aria-label="O que compartilhar">
          {SURFACES.map(({ surface, label, description, icon: Icon }) => (
            <button
              key={surface}
              type="button"
              disabled={picking}
              onClick={() => void pick(surface)}
              className={cn(
                'group flex items-center gap-3 rounded-xl border border-white/10 bg-surface-raised p-4 text-left transition-colors',
                'hover:border-blurple-soft hover:bg-surface-hover disabled:cursor-wait disabled:opacity-60',
              )}
            >
              <span className="grid size-11 shrink-0 place-items-center rounded-lg bg-surface-active text-fog group-hover:text-snow">
                <Icon className="size-5" />
              </span>

              <span className="min-w-0">
                <span className="block font-semibold text-snow">{label}</span>
                <span className="block text-xs text-greyple">{description}</span>
              </span>
            </button>
          ))}
        </div>

        <div className="relative aspect-video overflow-hidden rounded-xl border border-white/10 bg-black">
          <Preview track={shown} />

          {live ? (
            <span className="absolute top-3 left-3 flex items-center gap-1.5 rounded-full bg-ekko-red px-2.5 py-1 text-xs font-bold text-white">
              <Radio className="size-3.5" aria-hidden />
              AO VIVO
            </span>
          ) : preview ? (
            <span className="absolute top-3 left-3 rounded-full bg-black/70 px-2.5 py-1 text-xs font-bold text-white">
              Prévia: ninguém está vendo ainda
            </span>
          ) : null}
        </div>

        <QualitySettings preset={preset} hardware={hardware} />

        {error ? (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        ) : null}

        <div className="flex flex-wrap items-center justify-end gap-2">
          {live ? (
            <Button type="button" variant="destructive" onClick={stop}>
              <Square aria-hidden />
              Parar transmissão
            </Button>
          ) : (
            <>
              {preview ? (
                <Button
                  type="button"
                  variant="ghost"
                  onClick={() =>
                    void pick((preview.video.getSettings().displaySurface as ScreenSurface | undefined) ?? 'monitor')
                  }
                >
                  <RefreshCw aria-hidden />
                  Escolher outra
                </Button>
              ) : null}

              <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
                Cancelar
              </Button>

              <Button type="button" disabled={!preview} onClick={goLive}>
                <MonitorUp aria-hidden />
                Transmitir
              </Button>
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
