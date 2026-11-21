import { useEffect, useRef, type ReactNode } from 'react'

/** Plays a local video track muted, or shows `placeholder` while there is none. */
export const TrackPreview = ({ track, placeholder }: { track: MediaStreamTrack | null; placeholder: ReactNode }) => {
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
      <div className="grid size-full place-items-center p-4 text-center text-sm text-greyple">
        <span className="flex flex-col items-center gap-2">{placeholder}</span>
      </div>
    )
  }

  return <video ref={ref} autoPlay muted playsInline className="size-full object-contain" />
}
