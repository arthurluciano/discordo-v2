// Package voice is the native side of Discordo's calls: the audio devices, the Opus codec, the jitter buffers
// and mixing, and the WebRTC session with the server's SFU (docs/protocol.md, "Voz e vídeo").
// It knows nothing about the window: the app reads its state each frame and is woken when it changes.
package voice

import (
	"math"
	"sync"

	"github.com/kazzmir/opus-go/opus"
)

const (
	Rate  = 48000
	Frame = Rate / 50 // 20 ms, mono: what every device callback, the encoder and the mixer exchange
)

// Jitter buffer depths, in packets (20 ms each for browsers and us).
const (
	startDepth = 3  // waits for 60 ms before (re)starting a source
	maxDepth   = 15 // beyond 300 ms behind (clock drift, a burst), drops back to startDepth
)

// speakingRMS is use-audio-level.ts's SPEAKING_RMS (~-40 dBFS) on int16 samples.
const speakingRMS = 0.01 * 32768

// Mixer turns every remote audio track into one 48 kHz mono stream: per track a jitter buffer and an Opus decoder.
// Push is called from the network goroutines, Mix from the output device's thread.
type Mixer struct {
	mu      sync.Mutex
	sources map[*source]struct{}
	volumes map[string]float32 // by user: 0 to 2, 1 when absent
	deaf    bool
}

// source is one remote audio track.
type source struct {
	user    string
	dec     *opus.Decoder
	pkts    map[uint16][]byte // by RTP sequence number
	next    uint16            // the next packet to play
	playing bool
	pcm     []int16 // decoded and not played yet
	scratch []int16
	rms     float32 // the last frame's
}

func NewMixer() *Mixer {
	return &Mixer{sources: map[*source]struct{}{}, volumes: map[string]float32{}}
}

// Add starts a source for one of user's tracks; Remove it when the track ends.
func (m *Mixer) Add(user string) (*source, error) {
	dec, err := opus.NewDecoder(Rate, 1) // shortcut: stereo (screen audio) is downmixed, as the output is mono
	if err != nil {
		return nil, err
	}

	s := &source{user: user, dec: dec, pkts: map[uint16][]byte{}, scratch: make([]int16, 5760)}

	m.mu.Lock()
	m.sources[s] = struct{}{}
	m.mu.Unlock()

	return s, nil
}

func (m *Mixer) Remove(s *source) {
	m.mu.Lock()
	delete(m.sources, s)
	m.mu.Unlock()

	s.dec.Close()
}

// Push queues one RTP packet's payload. payload is copied.
func (m *Mixer) Push(s *source, seq uint16, payload []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s.playing && int16(seq-s.next) < 0 {
		return // too late: its slot was played (or concealed) already
	}

	s.pkts[seq] = append([]byte(nil), payload...)
}

func (m *Mixer) SetVolume(user string, v float32) {
	m.mu.Lock()
	m.volumes[user] = v
	m.mu.Unlock()
}

// SetDeafened silences the output; the buffers keep draining, so undeafening plays the present.
func (m *Mixer) SetDeafened(deaf bool) {
	m.mu.Lock()
	m.deaf = deaf
	m.mu.Unlock()
}

// Mix fills out (Frame samples) with the next 20 ms of everyone.
func (m *Mixer) Mix(out []int16) {
	var acc [Frame]float32
	var frame [Frame]int16

	m.mu.Lock()
	defer m.mu.Unlock()

	for s := range m.sources {
		s.read(frame[:])
		vol, ok := m.volumes[s.user]
		if !ok {
			vol = 1
		}

		var sum float64
		for i, v := range frame {
			sum += float64(v) * float64(v)
			acc[i] += float32(v) * vol
		}

		s.rms = float32(math.Sqrt(sum / Frame))
	}

	for i := range out[:Frame] {
		if m.deaf {
			out[i] = 0
			continue
		}

		out[i] = int16(max(-32768, min(32767, acc[i])))
	}
}

// Speaking reports which users were above the speaking level in the last mixed frame.
func (m *Mixer) Speaking(into map[string]bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for s := range m.sources {
		if s.rms > speakingRMS {
			into[s.user] = true
		}
	}
}

// read fills out with the source's next 20 ms: decoded packets, concealment for a lost one, silence while buffering.
func (s *source) read(out []int16) {
	for len(s.pcm) < len(out) {
		if !s.playing {
			if len(s.pkts) < startDepth {
				break
			}

			s.next, s.playing = s.oldest(), true
		}

		if len(s.pkts) > maxDepth {
			for len(s.pkts) > startDepth { // drop the oldest: we are too far behind
				delete(s.pkts, s.oldest())
			}

			s.next = s.oldest()
		}

		pkt, ok := s.pkts[s.next]
		if !ok && len(s.pkts) == 0 {
			s.playing = false // ran dry: buffer again
			break
		}

		delete(s.pkts, s.next)
		s.next++

		// A missing packet with later ones queued is lost: an empty packet asks the decoder to conceal 20 ms of it.
		size := len(s.scratch)
		if !ok {
			size = Frame
		}

		n, err := s.dec.Decode(pkt, s.scratch, size, false)
		if err != nil {
			continue // a corrupt packet is skipped like a lost one
		}

		s.pcm = append(s.pcm, s.scratch[:n]...)
	}

	n := copy(out, s.pcm)
	clear(out[n:])
	s.pcm = s.pcm[:copy(s.pcm, s.pcm[n:])]
}

func (s *source) oldest() uint16 {
	first, set := uint16(0), false
	for seq := range s.pkts {
		if !set || int16(seq-first) < 0 {
			first, set = seq, true
		}
	}

	return first
}

// Level is the RMS of a frame on a 0 to 1 scale, like the web's meter.
func Level(pcm []int16) float32 {
	var sum float64
	for _, v := range pcm {
		sum += float64(v) * float64(v)
	}

	return float32(math.Sqrt(sum/float64(max(1, len(pcm))))) / 32768
}
