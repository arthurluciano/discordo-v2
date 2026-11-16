import { describe, expect, it } from 'vitest'
import { nameTracks, parseOfferTracks } from './sdp'

// Shaped like the SFU's subscribe offer: one m-section per forwarded track, plus a removed one.
const OFFER = [
  'v=0', 'o=- 1 2 IN IP4 0.0.0.0', 's=-', 't=0 0', 'a=group:BUNDLE 0 1 2 3',
  'm=audio 9 UDP/TLS/RTP/SAVPF 111', 'c=IN IP4 0.0.0.0', 'a=mid:0', 'a=msid:user-a mic', 'a=sendonly', 'a=rtpmap:111 opus/48000/2',
  'm=video 9 UDP/TLS/RTP/SAVPF 102', 'c=IN IP4 0.0.0.0', 'a=mid:1', 'a=msid:user-b screen', 'a=sendonly',
  'm=audio 9 UDP/TLS/RTP/SAVPF 111', 'c=IN IP4 0.0.0.0', 'a=mid:2', 'a=inactive',
  'm=audio 0 UDP/TLS/RTP/SAVPF 111', 'a=mid:3', 'a=msid:user-c mic', 'a=sendonly',
  'm=audio 9 UDP/TLS/RTP/SAVPF 111', 'a=mid:4', 'a=msid:user-d something-else', 'a=sendonly',
  '',
].join('\r\n')

describe('parseOfferTracks', () => {
  it('maps active sections by mid to their owner and track name', () => {
    expect([...parseOfferTracks(OFFER)]).toEqual([
      ['0', { userId: 'user-a', name: 'mic' }],
      ['1', { userId: 'user-b', name: 'screen' }],
    ])
  })

  it('accepts LF line endings', () => {
    expect(parseOfferTracks(OFFER.replaceAll('\r\n', '\n')).get('1')).toEqual({ userId: 'user-b', name: 'screen' })
  })
})

describe('nameTracks', () => {
  it('names transceivers by mid and skips those without one yet', () => {
    expect(nameTracks([{ mid: '0', name: 'mic' }, { mid: null, name: 'camera' }, { mid: '2', name: 'screen' }]))
      .toEqual({ 0: 'mic', 2: 'screen' })
  })
})
