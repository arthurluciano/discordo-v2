import { useEffect, useState } from 'react'
import { useStore } from '@/lib/store'
import type { People } from './message-item'

export const TypingIndicator = ({ channelId, meId, people }: { channelId: string; meId: string; people: People }) => {
  const typing = useStore((s) => s.typing[channelId])
  const [now, setNow] = useState(() => Date.now())
  const active = Object.entries(typing ?? {}).filter(([userId, until]) => userId !== meId && until > now)

  // Re-render when the next "typing" expires.
  useEffect(() => {
    const next = Math.min(
      ...Object.entries(typing ?? {})
        .filter(([u]) => u !== meId)
        .map(([, until]) => until)
        .filter((t) => t > Date.now()),
    )
    if (!Number.isFinite(next)) return
    const timer = setTimeout(() => setNow(Date.now()), next - Date.now() + 50)
    return () => clearTimeout(timer)
  }, [typing, meId, now])

  const names = active.map(([userId]) => people.get(userId) ?? 'Alguém')
  const text =
    names.length === 0
      ? ''
      : names.length === 1
        ? `${names[0]} está digitando…`
        : names.length === 2
          ? `${names[0]} e ${names[1]} estão digitando…`
          : 'Várias pessoas estão digitando…'

  return (
    <p aria-live="polite" className="h-5 shrink-0 truncate px-5 text-xs text-fog">
      {text}
    </p>
  )
}
