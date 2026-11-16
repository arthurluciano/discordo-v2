import { afterEach, describe, expect, it, vi } from 'vitest'
import { createPionTransport } from './pion'

// Just enough RTCPeerConnection for the signaling logic: mids are assigned on setLocalDescription.
class FakePeer {
  connectionState = 'new'
  iceGatheringState = 'complete'
  localDescription: { sdp: string } | null = null
  remote: string[] = []
  transceivers: { mid: string | null; sender: { track: unknown }; receiver: { track: unknown }; stop: () => void }[] = []
  addEventListener() {}
  addTransceiver(track: unknown) {
    const t = { mid: null, sender: { track }, receiver: { track: {} }, stop: vi.fn() }
    this.transceivers.push(t)
    return t
  }
  async setLocalDescription() {
    this.transceivers.forEach((t, i) => { t.mid ??= String(i) })
    this.localDescription = { sdp: `local-${this.transceivers.length}` }
  }
  async setRemoteDescription({ sdp }: { sdp: string }) { this.remote.push(sdp) }
  getTransceivers() { return this.transceivers }
  close() {}
}

const flush = () => new Promise((r) => setTimeout(r, 0))
const track = (kind: string) => ({ kind }) as MediaStreamTrack

afterEach(() => vi.unstubAllGlobals())

describe('pion transport', () => {
  it('names published tracks by mid and never overlaps publish offers', async () => {
    vi.stubGlobal('RTCPeerConnection', FakePeer)
    const send = vi.fn(() => true)
    const t = createPionTransport(send, { onRemoteTracks: vi.fn(), onState: vi.fn(), onError: vi.fn() })

    void t.publish('mic', track('audio'))
    void t.publish('screen-audio', track('audio'))
    await flush()
    expect(send.mock.calls).toEqual([['rtc_offer', { sdp: 'local-2', tracks: { 0: 'mic', 1: 'screen-audio' } }]])

    t.handle({ t: 'RTC_ANSWER', d: { sdp: 'answer-1' } })
    await flush()
    expect(send).toHaveBeenCalledTimes(2) // the second offer only after the first answer
    t.handle({ t: 'RTC_ANSWER', d: { sdp: 'answer-2' } })
    await flush()
    t.close()
  })

  it('answers the server offer and reports who owns each received track', async () => {
    vi.stubGlobal('RTCPeerConnection', class extends FakePeer {
      override async setRemoteDescription(d: { sdp: string }) {
        await super.setRemoteDescription(d)
        this.transceivers = [{ mid: '0', sender: { track: null }, receiver: { track: 'remote-mic' }, stop: vi.fn() }]
      }
    })
    const send = vi.fn(() => true)
    const onRemoteTracks = vi.fn()
    const t = createPionTransport(send, { onRemoteTracks, onState: vi.fn(), onError: vi.fn() })

    t.handle({ t: 'RTC_OFFER', d: { sdp: 'v=0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\na=mid:0\r\na=msid:user-a mic\r\na=sendonly\r\n' } })
    await flush()
    expect(send).toHaveBeenCalledWith('rtc_answer', { sdp: 'local-1' })
    expect(onRemoteTracks).toHaveBeenCalledWith([{ userId: 'user-a', name: 'mic', track: 'remote-mic' }])
  })
})
