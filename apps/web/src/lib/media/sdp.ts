// SDP helpers for the pion transport.
import type { TrackName } from '@/lib/wire'

export type TrackOwner = { userId: string; name: TrackName }

const TRACK_NAMES: readonly string[] = ['mic', 'screen-audio', 'camera', 'screen'] satisfies TrackName[]

/** What each m-section of the server's subscribe offer carries: mid -> (owner user ID, track name).
 * The SFU sets `a=msid:<owner user ID> <track name>`; sections it stopped sending are left out. */
export const parseOfferTracks = (sdp: string): Map<string, TrackOwner> => {
  const tracks = new Map<string, TrackOwner>()
  for (const section of sdp.split(/\r?\n(?=m=)/).slice(1)) {
    const lines = section.split(/\r?\n/)
    if (/^m=\S+ 0 /.test(lines[0] ?? '')) continue // port 0: rejected or stopped
    if (lines.some((l) => l === 'a=inactive' || l === 'a=recvonly')) continue
    const mid = lines
      .find((l) => l.startsWith('a=mid:'))
      ?.slice(6)
      .trim()
    const msid = lines
      .find((l) => l.startsWith('a=msid:'))
      ?.slice(7)
      .trim()
      .split(/\s+/)
    const [userId, name] = msid ?? []
    if (mid && userId && name && TRACK_NAMES.includes(name)) tracks.set(mid, { userId, name: name as TrackName })
  }

  return tracks
}

/** The `tracks` field of `rtc_offer`: names the transceivers we publish by mid (browsers cannot choose track IDs). */
export const nameTracks = (published: Iterable<{ mid: string | null; name: TrackName }>): Record<string, TrackName> => {
  const names: Record<string, TrackName> = {}
  for (const { mid, name } of published) if (mid) names[mid] = name

  return names
}
