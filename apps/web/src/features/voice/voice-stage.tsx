// CONTRACT (implemented by the voice area): the call view of a voice channel or DM.
import { useCallback, useEffect, useId, useMemo, useRef, useState, type ComponentType, type RefObject } from 'react'
import { AnimatePresence, LayoutGroup, motion, useTransform } from 'motion/react'
import { ArrowLeft, Ellipsis, Eye, EyeOff, HeadphoneOff, LayoutGrid, Maximize2, MicOff, Minimize2, MonitorUp, PhoneCall } from 'lucide-react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuLabel, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { Avatar } from '@/design/avatar'
import { SPRING_SNAP, useTransition } from '@/design/motion'
import { displayName, useMe, useUser } from '@/features/identity/users'
import type { Quality } from '@/lib/media/transport'
import { has, Permission } from '@/lib/permissions'
import { useStore } from '@/lib/store'
import { cn } from '@/lib/utils'
import type { VoiceState } from '@/lib/wire'
import { callPermissions, watchKey } from './call'
import { useAudioLevel } from './use-audio-level'
import { ControlsBar } from './voice-controls'
import { useCallTitle } from './voice-dock'
import { useVoice, voice } from './voice-session'

export type VoiceStageProps = {
  channelId: string
  /** Mobile: back to the navigation pane. */
  onBack: () => void
}

type Icon = ComponentType<{ className?: string; 'aria-hidden'?: boolean }>
type Layout = 'focal' | 'grid' | 'strip'

type TileItem = {
  id: string
  state: VoiceState
  kind: 'person' | 'screen'
  video: MediaStreamTrack | null
  /** Published by someone else and not being received: "Assistir" subscribes. */
  watchable: boolean
  /** Being received: can stop watching. */
  watched: boolean
  own: boolean
}

const LAYOUT: Record<Layout, string> = { focal: 'size-full rounded-2xl', grid: 'aspect-video w-full rounded-xl', strip: 'aspect-video w-full rounded-lg' }

const StageButton = ({ icon: Icon, label, onClick }: { icon: Icon; label: string; onClick: () => void }) => (
  <Tooltip>
    <TooltipTrigger asChild>
      <Button variant="outline" size="icon-sm" onClick={onClick} aria-label={label} className="border-white/10 bg-black/60 text-white backdrop-blur-sm hover:bg-black/80">
        <Icon aria-hidden />
      </Button>
    </TooltipTrigger>
    <TooltipContent>{label}</TooltipContent>
  </Tooltip>
)

const VideoView = ({ track, contain, mirror }: { track: MediaStreamTrack; contain: boolean; mirror: boolean }) => {
  const ref = useRef<HTMLVideoElement>(null)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    el.srcObject = new MediaStream([track])
    return () => { el.srcObject = null }
  }, [track])
  return <video ref={ref} autoPlay playsInline muted className={cn('size-full', contain ? 'object-contain' : 'object-cover', mirror && '-scale-x-100')} />
}

const useMemberNickname = (state: VoiceState) =>
  useStore((s) => (state.guild_id ? s.guilds.find((g) => g.id === state.guild_id)?.members.find((m) => m.user.id === state.user_id)?.nickname ?? '' : ''))

const QUALITIES: [Quality, string][] = [['auto', 'Auto'], ['high', 'Alta'], ['medium', 'Média'], ['low', 'Baixa']]

/** Per-tile options, also on right-click: someone's volume, and which quality of their video to receive. */
type TileOptions = {
  /** Null: not adjustable (our own tile, a screen, or not in the call). */
  volume: number | null
  onVolume: (volume: number) => void
  /** Null: not receiving this video. */
  quality: Quality | null
  onQuality: (quality: Quality) => void
}

const TileMenu = ({ name, open, onOpenChange, volume, onVolume, quality, onQuality }: TileOptions & { name: string; open: boolean; onOpenChange: (open: boolean) => void }) => {
  const id = useId()
  const percent = Math.round((volume ?? 1) * 100)
  return (
    <DropdownMenu open={open} onOpenChange={onOpenChange}>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="icon-sm" aria-label={`Opções de ${name}`}
          className="absolute right-2 top-2 z-20 border-white/10 bg-black/60 text-white opacity-0 backdrop-blur-sm transition-opacity hover:bg-black/80 focus-visible:opacity-100 group-hover/tile:opacity-100 data-[state=open]:opacity-100 pointer-coarse:opacity-100">
          <Ellipsis aria-hidden />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        {volume !== null ? (
          <div className="grid gap-1.5 px-2 py-1.5">
            <label htmlFor={id} className="flex justify-between text-xs font-semibold">Volume<span data-numeric>{percent}%</span></label>
            <input id={id} type="range" min={0} max={200} step={5} value={percent} aria-valuetext={`${percent}%`}
              onChange={(e) => onVolume(Number(e.target.value) / 100)} className="w-full accent-blurple" />
          </div>
        ) : null}
        {volume !== null && quality !== null ? <DropdownMenuSeparator /> : null}
        {quality !== null ? (
          <>
            <DropdownMenuLabel>Qualidade do vídeo</DropdownMenuLabel>
            <DropdownMenuRadioGroup value={quality} onValueChange={(q) => onQuality(q as Quality)}>
              {QUALITIES.map(([q, label]) => <DropdownMenuRadioItem key={q} value={q} onSelect={(e) => e.preventDefault()}>{label}</DropdownMenuRadioItem>)}
            </DropdownMenuRadioGroup>
          </>
        ) : null}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

type TileProps = { item: TileItem; layout: Layout; speech: MediaStreamTrack | null; onFocus?: (() => void) | undefined; onWatch: () => void; options: TileOptions }

const Tile = ({ item, layout, speech, onFocus, onWatch, options }: TileProps) => {
  const [menu, setMenu] = useState(false)
  const hasMenu = options.volume !== null || options.quality !== null
  const user = useUser(item.state.user_id)
  const nickname = useMemberNickname(item.state)
  const name = user ? displayName(user, nickname) : 'Alguém'
  const transition = useTransition(SPRING_SNAP)
  const level = useAudioLevel(item.kind === 'person' ? speech : null)
  const ring = useTransform(level, (l) => `${(l > 0 ? 2 + l * 4 : 0).toFixed(2)}px solid var(--media-speaking)`)
  const screen = item.kind === 'screen'
  const label = `${name}${screen ? ', compartilhando tela' : ''}`

  return (
    <motion.article layout="position" layoutId={`tile-${item.id}`} initial={{ opacity: 0, scale: 0.98 }} animate={{ opacity: 1, scale: 1 }} exit={{ opacity: 0, scale: 0.98 }} transition={transition}
      aria-label={label} onContextMenu={hasMenu ? (e) => { e.preventDefault(); setMenu(true) } : undefined}
      className={cn('group/tile relative isolate overflow-hidden bg-surface-sidebar', screen && 'bg-surface-floating', LAYOUT[layout])}>
      {item.video ? (
        <VideoView track={item.video} contain={screen} mirror={item.own && !screen} />
      ) : (
        <span className="grid size-full place-items-center bg-surface-raised">
          {screen ? <MonitorUp aria-hidden className="size-10 text-fog" /> : (
            <motion.span className="rounded-full" style={{ outline: ring, outlineOffset: '3px' }}>
              <Avatar userId={item.state.user_id} displayName={name} avatarUrl={user?.avatar_url} size={layout === 'strip' ? 'medium' : 'large'} />
            </motion.span>
          )}
        </span>
      )}

      {/* Covers the tile under its buttons: clicking anywhere focuses it without nesting buttons. */}
      {onFocus ? <button type="button" onClick={onFocus} aria-label={`Focar ${label}`} className="absolute inset-0 z-10 cursor-pointer rounded-[inherit] outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-blurple-soft" /> : null}

      {screen ? (
        <span className={cn('pointer-events-none absolute left-2 top-2 z-20 flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] font-bold uppercase', item.video ? 'bg-media-screenshare text-media-screenshare-foreground' : 'bg-black/65 text-fog')}>
          <MonitorUp aria-hidden className="size-3" />Tela
        </span>
      ) : null}

      {hasMenu ? <TileMenu name={name} open={menu} onOpenChange={setMenu} {...options} /> : null}

      {item.watchable ? (
        <span className="absolute inset-x-0 bottom-10 z-20 flex justify-center">
          <Button size={layout === 'strip' ? 'xs' : 'sm'} onClick={onWatch}><Eye aria-hidden />{screen ? 'Assistir' : 'Ver câmera'}</Button>
        </span>
      ) : null}

      <span className={cn('pointer-events-none absolute bottom-2 left-2 z-20 flex max-w-[calc(100%-1rem)] items-center gap-1.5 rounded-md bg-black/65 px-2 py-1 text-white backdrop-blur-sm', layout === 'strip' ? 'text-[11px]' : 'text-xs')}>
        {!screen && item.state.muted ? <MicOff aria-label="mutado" className="size-3 shrink-0 text-media-muted" /> : null}
        {!screen && item.state.deafened ? <HeadphoneOff aria-label="ensurdecido" className="size-3 shrink-0 text-media-deafened" /> : null}
        <span className="truncate font-semibold">{name}</span>
      </span>
    </motion.article>
  )
}

const isTyping = (target: EventTarget | null) =>
  target instanceof HTMLElement && (target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName))

/** CSS covers the window everywhere; the Fullscreen API, where available, takes the whole screen. */
const useStageFullscreen = (stage: RefObject<HTMLElement | null>) => {
  const [fullscreen, setFullscreen] = useState(false)

  const toggle = useCallback(() => {
    if (fullscreen) {
      setFullscreen(false)
      if (document.fullscreenElement) document.exitFullscreen().catch(() => {})
    } else {
      setFullscreen(true)
      if (document.fullscreenEnabled) stage.current?.requestFullscreen().catch(() => {})
    }
  }, [fullscreen, stage])

  useEffect(() => {
    // The browser's Esc leaves the Fullscreen API without our button.
    const follow = () => { if (!document.fullscreenElement) setFullscreen(false) }
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey || isTyping(e.target)) return
      if (e.key === 'Escape' && fullscreen) toggle()
      else if (e.key.toLowerCase() === 'f') toggle()
    }
    document.addEventListener('fullscreenchange', follow)
    window.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('fullscreenchange', follow)
      window.removeEventListener('keydown', onKey)
    }
  }, [fullscreen, toggle])

  return { fullscreen, toggle }
}

const peopleCount = (n: number) => `${n} ${n === 1 ? 'pessoa' : 'pessoas'}`

export const VoiceStage = ({ channelId, onBack }: VoiceStageProps) => {
  const v = useVoice((s) => ({ channelId: s.channelId, connection: s.connection, remote: s.remote, camera: s.camera, screen: s.screen, mic: s.mic, watching: s.watching, volumes: s.volumes, qualities: s.qualities, error: s.error }))
  const me = useMe()
  const title = useCallTitle(channelId)
  const allVoice = useStore((s) => s.voice)
  const perms = useStore((s) => callPermissions(s, channelId))
  const stageRef = useRef<HTMLElement>(null)
  const { fullscreen, toggle: toggleFullscreen } = useStageFullscreen(stageRef)
  const [pinned, setPinned] = useState<string | null>(null)
  const ours = v.channelId === channelId
  const inCall = ours && (v.connection === 'connecting' || v.connection === 'connected')

  const participants = useMemo(() => Object.values(allVoice).filter((s) => s.channel_id === channelId), [allVoice, channelId])

  const remoteTrack = (userId: string, name: string) => v.remote.find((r) => r.userId === userId && r.name === name)?.track ?? null

  const tiles: TileItem[] = []
  for (const state of participants) {
    const own = state.user_id === me?.id
    for (const kind of ['screen', 'person'] as const) {
      const track = kind === 'screen' ? 'screen' : 'camera'
      const published = own && inCall ? (kind === 'screen' ? v.screen : v.camera) !== null : state.tracks.includes(track)
      if (kind === 'screen' && !published) continue
      const watched = !own && inCall && published && v.watching[watchKey(state.user_id, track)] === true
      const video = own && inCall ? (kind === 'screen' ? v.screen : v.camera) : watched ? remoteTrack(state.user_id, track) : null
      tiles.push({ id: `${state.user_id}/${kind}`, state, kind, video, own, watched, watchable: !own && inCall && published && !watched })
    }
  }

  const focal = tiles.find((t) => t.id === pinned && t.video) ?? null
  const others = tiles.filter((t) => t !== focal)
  const speechOf = (t: TileItem) => (t.own ? v.mic : remoteTrack(t.state.user_id, 'mic'))
  const watchTile = (t: TileItem, on: boolean) => {
    voice.watch(t.state.user_id, t.kind === 'screen' ? 'screen' : 'camera', on)
    setPinned(on ? t.id : null)
  }

  const optionsOf = (t: TileItem): TileOptions => {
    const track = t.kind === 'screen' ? 'screen' : 'camera'
    return {
      volume: !t.own && inCall && t.kind === 'person' ? (v.volumes[t.state.user_id] ?? 1) : null,
      onVolume: (volume) => voice.setVolume(t.state.user_id, volume),
      quality: t.watched ? (v.qualities[watchKey(t.state.user_id, track)] ?? 'auto') : null,
      onQuality: (quality) => voice.setQuality(t.state.user_id, track, quality),
    }
  }

  const renderTile = (t: TileItem, layout: Layout) => (
    <Tile key={t.id} item={t} layout={layout} speech={speechOf(t)} onWatch={() => watchTile(t, true)} onFocus={t.video ? () => setPinned(t.id) : undefined} options={optionsOf(t)} />
  )

  const canConnect = has(perms, Permission.Connect)

  return (
    <LayoutGroup>
      <section ref={stageRef} aria-label="Palco da chamada"
        className={cn('relative flex h-full min-h-0 flex-1 flex-col gap-3 bg-surface-rail p-3 sm:p-4', fullscreen && 'fixed inset-0 z-50 bg-surface-floating')}>
        <header className="flex shrink-0 items-center gap-1.5">
          <Button variant="ghost" size="icon-sm" className="md:hidden" onClick={onBack} aria-label="Voltar"><ArrowLeft aria-hidden /></Button>
          <div className="min-w-0 flex-1">
            <h2 className="truncate font-bold text-snow">{title}</h2>
            <p className="telemetry truncate text-greyple" data-numeric>{peopleCount(participants.length)}</p>
          </div>
          {focal ? <StageButton icon={LayoutGrid} label="Ver todos em grade" onClick={() => setPinned(null)} /> : null}
          {focal && focal.watched ? <StageButton icon={EyeOff} label="Parar de assistir" onClick={() => watchTile(focal, false)} /> : null}
          <StageButton icon={fullscreen ? Minimize2 : Maximize2} label={fullscreen ? 'Sair da tela cheia (Esc)' : 'Tela cheia (F)'} onClick={toggleFullscreen} />
        </header>

        {tiles.length === 0 ? (
          <div className="grid flex-1 place-items-center text-center text-greyple">Ninguém na chamada ainda.</div>
        ) : focal ? (
          <>
            <div className="min-h-0 flex-1">{renderTile(focal, 'focal')}</div>
            {others.length > 0 ? (
              <ul aria-label="Outros participantes" className="flex shrink-0 gap-2 overflow-x-auto pb-1">
                <AnimatePresence initial={false} mode="popLayout">
                  {others.map((t) => <motion.li key={t.id} layout exit={{ opacity: 0, scale: 0.96 }} className="w-32 shrink-0 sm:w-40 lg:w-48">{renderTile(t, 'strip')}</motion.li>)}
                </AnimatePresence>
              </ul>
            ) : null}
          </>
        ) : (
          <ul aria-label="Participantes" className="grid min-h-0 flex-1 grid-cols-[repeat(auto-fit,minmax(min(100%,16rem),1fr))] content-center gap-3 overflow-y-auto">
            <AnimatePresence initial={false} mode="popLayout">
              {tiles.map((t) => <motion.li key={t.id} layout exit={{ opacity: 0, scale: 0.96 }}>{renderTile(t, 'grid')}</motion.li>)}
            </AnimatePresence>
          </ul>
        )}

        {ours && v.error ? (
          <Alert variant="destructive" className="shrink-0"><AlertDescription className="flex items-center gap-2">
            <span className="flex-1">{v.error}</span>
            <Button variant="ghost" size="xs" onClick={voice.dismissError}>Fechar</Button>
          </AlertDescription></Alert>
        ) : null}

        <footer className="flex shrink-0 justify-center">
          {inCall ? <ControlsBar channelId={channelId} /> : canConnect ? (
            <Button size="lg" onClick={() => voice.join(channelId)}><PhoneCall aria-hidden />Entrar na chamada</Button>
          ) : <p className="text-sm text-greyple">Você não tem permissão para entrar nesta chamada.</p>}
        </footer>
      </section>
    </LayoutGroup>
  )
}
