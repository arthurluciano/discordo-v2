package voice

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/kazzmir/opus-go/opus"
	"github.com/pion/rtcp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"
)

// Video tracks a user publishes (media.Camera, media.Screen on the server).
const (
	TrackScreen      = "screen"
	TrackScreenAudio = "screen-audio"
	TrackCamera      = "camera"
)

// h264 is what we send: Constrained Baseline in packetization mode 1, which every browser decodes. The SFU forwards
// it untouched, so viewers get exactly this.
var h264 = webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000,
	SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f"}

const pliEvery = 500 * time.Millisecond // keyframe requests at most this often per track

// videoState is the session's video: what it shows and what it shares. Guarded by Session.mu.
type videoState struct {
	frames   map[string]Picture // by user/track: the latest picture
	notes    map[string]string  // by user/track: why it does not show
	screen   *screenOut
	screenOn bool           // StartScreen ran and StopScreen did not
	capture  *ScreenCapture // what PickScreen chose: my preview, then what StartScreen sends
}

// screenOut is my screen going out: its track, the encoder made for the pictures' size, and its transceiver.
type screenOut struct {
	track   *webrtc.TrackLocalStaticSample
	tr      *webrtc.RTPTransceiver
	enc     *Encoder
	fps     int
	bitrate int
	last    time.Time
	lastPic Picture // retained: sent again on a keyframe request, as a still screen sends nothing

	// Its sound, in 20 ms Opus packets; sound comes on one thread (ScreenCaptureKit's queue), under soundMu.
	sound   *webrtc.TrackLocalStaticSample
	soundTr *webrtc.RTPTransceiver
	soundMu sync.Mutex
	opus    *opus.Encoder
	pcm     []int16 // what is left of the last sample buffer, short of a packet
	packet  []byte
}

// closeSound frees the sound's encoder; nothing more is sent after it.
func (out *screenOut) closeSound() {
	out.soundMu.Lock()
	defer out.soundMu.Unlock()

	if out.opus != nil {
		out.opus.Close()
	}

	out.opus, out.sound = nil, nil
}

// SendScreenSound sends my screen's sound (48 kHz stereo, interleaved) while my screen goes out.
func (s *Session) SendScreenSound(pcm []int16) {
	s.mu.Lock()
	out := s.vid.screen
	s.mu.Unlock()

	if out == nil || out.sound == nil {
		return // a preview: nobody hears it
	}

	out.soundMu.Lock()
	defer out.soundMu.Unlock()

	if out.opus == nil {
		enc, err := opus.NewEncoder(Rate, 2, opus.ApplicationAudio)
		if err != nil {
			return
		}

		enc.SetBitrate(128_000) // presets.ts's SCREEN_AUDIO_ENCODING
		out.opus, out.packet = enc, make([]byte, 4000)
	}

	out.pcm = append(out.pcm, pcm...)
	for len(out.pcm) >= 2*Frame {
		n, err := out.opus.Encode(out.pcm[:2*Frame], Frame, out.packet)
		out.pcm = out.pcm[2*Frame:]
		if err == nil {
			_ = out.sound.WriteSample(media.Sample{Data: out.packet[:n], Duration: 20 * time.Millisecond})
		}
	}

	out.pcm = append(out.pcm[:0], out.pcm...) // keeps the buffer from creeping along its array
}

func videoKey(user, track string) string { return user + "/" + track }

// Watch starts (or stops) receiving someone's camera or screen: the server forwards video only on request.
func (s *Session) Watch(userID, track string, on bool) {
	s.send("rtc_subscribe", map[string]any{"user_id": userID, "track": track, "on": on})
	if on {
		return
	}

	s.mu.Lock()
	key := videoKey(userID, track)
	s.vid.frames[key].Release()
	delete(s.vid.frames, key)
	delete(s.vid.notes, key)
	s.mu.Unlock()
}

// WatchQuality asks for someone's track at a quality: auto, high, medium or low (the server's simulcast layer).
func (s *Session) WatchQuality(userID, track, quality string) {
	s.send("rtc_subscribe", map[string]any{"user_id": userID, "track": track, "on": true, "quality": quality})
}

// Frame is the latest picture of a user's track, retained for the caller (release it), or 0. My own screen's key
// is my user ID: the preview of what I share.
func (s *Session) Frame(userID, track string) Picture {
	s.mu.Lock()
	defer s.mu.Unlock()

	p := s.vid.frames[videoKey(userID, track)]
	if p != 0 {
		p.Retain()
	}

	return p
}

// VideoNote says why a watched track shows nothing ("" when it shows, or is on its way).
func (s *Session) VideoNote(userID, track string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.vid.notes[videoKey(userID, track)]
}

// setFrame keeps p as a track's latest picture (taking it over) and wakes the app.
func (s *Session) setFrame(key string, p Picture) {
	s.mu.Lock()
	old := s.vid.frames[key]
	if s.closed {
		old, p = p, 0
	}

	if p != 0 {
		s.vid.frames[key] = p
		delete(s.vid.notes, key)
	}
	s.mu.Unlock()

	old.Release()
	s.changed()
}

func (s *Session) setNote(key, note string) {
	s.mu.Lock()
	s.vid.notes[key] = note
	s.mu.Unlock()
	s.changed()
}

// receiveVideo depacketizes and decodes one remote video track for as long as it lasts.
func (s *Session) receiveVideo(remote *webrtc.TrackRemote) {
	key := videoKey(remote.StreamID(), remote.ID())
	defer func() {
		s.mu.Lock()
		s.vid.frames[key].Release()
		delete(s.vid.frames, key)
		s.mu.Unlock()
		s.changed()
	}()

	mime := remote.Codec().MimeType
	var dec *Decoder
	var err error
	if strings.EqualFold(mime, webrtc.MimeTypeH264) {
		dec, err = NewDecoder(func(p Picture) { s.setFrame(key, p) })
	} else {
		err = errors.New("formato " + strings.TrimPrefix(mime, "video/") + " ainda não é exibido no app nativo")
	}

	if err != nil {
		s.setNote(key, "Não dá para mostrar este vídeo: "+err.Error()+".")
		for { // keep reading so the connection's buffers drain
			if _, _, err := remote.ReadRTP(); err != nil {
				return
			}
		}
	}
	defer dec.Close()

	sb := samplebuilder.New(512, &codecs.H264Packet{}, 90000)
	var lastPLI time.Time
	pli := func() {
		if time.Since(lastPLI) < pliEvery {
			return
		}

		lastPLI = time.Now()
		s.mu.Lock()
		sub := s.sub
		s.mu.Unlock()

		if sub != nil {
			_ = sub.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(remote.SSRC())}})
		}
	}

	// A still screen sends nothing until asked: ask for a keyframe every second until a picture shows.
	shown := make(chan struct{})
	var showOnce sync.Once
	defer showOnce.Do(func() { close(shown) })

	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()

		for {
			s.mu.Lock()
			sub := s.sub
			s.mu.Unlock()

			if sub != nil {
				_ = sub.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(remote.SSRC())}})
			}

			select {
			case <-shown:
				return
			case <-s.ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()

	waiting := true // for a keyframe: until one decodes, deltas are noise
	for {
		pkt, _, err := remote.ReadRTP()
		if err != nil {
			return
		}

		sb.Push(pkt)
		for sample := sb.Pop(); sample != nil; sample = sb.Pop() {
			if sample.PrevDroppedPackets > 0 && !hasIDR(sample.Data) {
				waiting = true // a lost piece breaks every picture until the next keyframe
			}

			if waiting && !hasIDR(sample.Data) {
				pli()
				continue
			}

			if err := dec.Decode(sample.Data); err != nil {
				waiting = true
				pli()
				continue
			}

			waiting = false
			showOnce.Do(func() { close(shown) })
		}
	}
}

// hasIDR says whether an access unit carries a keyframe (an IDR slice, NAL type 5).
func hasIDR(au []byte) bool {
	for _, nal := range splitAnnexB(au) {
		if nal[0]&0x1f == 5 {
			return true
		}
	}

	return false
}

// PickScreen shows the system's picker for a screen (or a window); what the user picks shows as my preview,
// Frame(me, TrackScreen), and goes out once StartScreen runs. Picking again while one runs replaces it when the new
// one's first picture comes. onEnd (any thread) says the picker was cancelled (ErrPickerCancelled) or the capture
// ended by itself: then a share that was going out stops.
func (s *Session) PickScreen(opt ScreenOptions, onEnd func(error)) error {
	var self *ScreenCapture // set under s.mu before any picture can come (only after the user picks)
	c, err := PickScreen(opt, func(p Picture) { s.screenFrame(self, p) }, s.SendScreenSound, func(err error) {
		s.mu.Lock()
		current := s.vid.capture == self
		if current {
			s.vid.capture = nil
		}
		s.mu.Unlock()

		if current && !errors.Is(err, ErrPickerCancelled) {
			go s.StopScreen() // it negotiates: not on ScreenCaptureKit's thread
		}

		if onEnd != nil {
			onEnd(err)
		}
	})
	if err != nil {
		return err
	}

	s.mu.Lock()
	self = c
	s.mu.Unlock()

	return nil
}

// screenFrame takes a picture of a capture: the first one of a new capture stops the old.
func (s *Session) screenFrame(c *ScreenCapture, p Picture) {
	s.mu.Lock()
	var old *ScreenCapture
	if s.vid.capture != c {
		old, s.vid.capture = s.vid.capture, c
	}

	live, me, closed := s.vid.screen != nil, s.me, s.closed
	s.mu.Unlock()

	if old != nil {
		old.Stop()
	}

	switch {
	case closed:
		c.Stop()
	case live:
		_ = s.SendScreen(p)
	default:
		s.setFrame(videoKey(me, TrackScreen), p.Retain())
	}
}

// SetScreenOptions changes the size, rate and bitrate of my screen, previewed or going out.
func (s *Session) SetScreenOptions(opt ScreenOptions, bitrate int) {
	s.mu.Lock()
	c, out := s.vid.capture, s.vid.screen
	var enc *Encoder
	if out != nil {
		out.fps, out.bitrate = max(1, opt.FPS), bitrate
		enc, out.enc = out.enc, nil // the next picture makes an encoder for these
	}
	s.mu.Unlock()

	if enc != nil {
		enc.Close()
	}

	if c != nil {
		c.Update(opt)
	}
}

// Previewing says whether a capture runs (picked, maybe not going out yet).
func (s *Session) Previewing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.vid.capture != nil
}

// StartScreen publishes my screen: a "screen" track in H.264; SendScreen feeds it pictures. It negotiates with the
// server, so it blocks: not on the UI thread.
func (s *Session) StartScreen(fps, bitrate int) error {
	track, err := webrtc.NewTrackLocalStaticSample(h264, TrackScreen, "discordo")
	if err != nil {
		return err
	}

	s.pubMu.Lock()
	pub, err := s.ensurePub()
	if err != nil {
		s.pubMu.Unlock()
		return err
	}

	tr, err := pub.AddTransceiverFromTrack(track, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
	if err != nil {
		s.pubMu.Unlock()
		return err
	}

	// The sound goes on its own track, always there: the SFU publishes it with its first packet, if sound comes.
	sound, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: Rate, Channels: 2}, TrackScreenAudio, "discordo")
	if err != nil {
		s.pubMu.Unlock()
		return err
	}

	soundTr, err := pub.AddTransceiverFromTrack(sound, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
	if err != nil {
		s.pubMu.Unlock()
		return err
	}

	go drainRTCP(soundTr.Sender())

	out := &screenOut{track: track, tr: tr, fps: fps, bitrate: bitrate, sound: sound, soundTr: soundTr}
	s.mu.Lock()
	s.pubNames[soundTr] = TrackScreenAudio
	s.pubNames[tr] = TrackScreen
	s.vid.screen, s.vid.screenOn = out, true
	s.mu.Unlock()
	s.pubMu.Unlock()

	// A viewer that lost pictures (or just started) asks for a keyframe; the SFU passes it on to us.
	go func() {
		for {
			pkts, _, err := tr.Sender().ReadRTCP()
			if err != nil {
				return
			}

			for _, p := range pkts {
				switch p.(type) {
				case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
					s.mu.Lock()
					enc, pic := out.enc, out.lastPic.Retain()
					s.mu.Unlock()

					if enc != nil {
						enc.ForceKeyframe()
					}

					if enc != nil && pic != 0 {
						_ = enc.Encode(pic)
					}

					pic.Release()
				}
			}
		}
	}()

	// A still screen gives no pictures: the last one goes again every second, so a viewer who arrives later (whose
	// track starts with the first packet after the SFU adds them) gets something to ask a keyframe on.
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()

		for {
			select {
			case <-s.ctx.Done():
				return
			case <-tick.C:
			}

			s.mu.Lock()
			if s.vid.screen != out {
				s.mu.Unlock()
				return
			}

			var enc *Encoder
			var pic Picture
			if time.Since(out.last) >= time.Second {
				enc, pic = out.enc, out.lastPic.Retain()
				out.last = time.Now()
			}
			s.mu.Unlock()

			if enc != nil && pic != 0 {
				_ = enc.Encode(pic)
			}

			pic.Release()
		}
	}()

	return s.negotiate()
}

// SendScreen encodes and sends one picture of my screen (which it does not take over), and keeps it as my preview.
// Pictures beyond the frame rate are dropped.
func (s *Session) SendScreen(p Picture) error {
	s.mu.Lock()
	out, me := s.vid.screen, s.me
	if out == nil || time.Since(out.last) < time.Second/time.Duration(out.fps+out.fps/10) {
		s.mu.Unlock()
		return nil
	}

	out.last = time.Now()
	enc := out.enc
	out.lastPic.Release()
	out.lastPic = p.Retain()
	s.mu.Unlock()

	s.setFrame(videoKey(me, TrackScreen), p.Retain())

	// A new size (another window, a resized one) needs a new encoder: its first frame is a keyframe.
	if w, h := p.Size(); enc == nil || func() bool { ew, eh := enc.Size(); return ew != w || eh != h }() {
		next, err := NewEncoder(w, h, out.fps, out.bitrate, func(au []byte, key bool) {
			_ = out.track.WriteSample(media.Sample{Data: au, Duration: time.Second / time.Duration(out.fps)})
		})
		if err != nil {
			return err
		}

		s.mu.Lock()
		old := out.enc
		out.enc = next
		s.mu.Unlock()

		if old != nil {
			old.Close()
		}

		enc = next
	}

	return enc.Encode(p)
}

// StopScreen stops my screen: its capture (the preview) and what goes out. It negotiates when it was going out: not
// on the UI thread.
func (s *Session) StopScreen() error {
	s.mu.Lock()
	out, me, c := s.vid.screen, s.me, s.vid.capture
	s.vid.screen, s.vid.screenOn, s.vid.capture = nil, false, nil
	if out != nil {
		delete(s.pubNames, out.tr)
		delete(s.pubNames, out.soundTr)
		out.lastPic.Release()
		out.lastPic = 0
	}

	pub := s.pub
	s.vid.frames[videoKey(me, TrackScreen)].Release()
	delete(s.vid.frames, videoKey(me, TrackScreen))
	s.mu.Unlock()
	s.changed()

	if c != nil {
		c.Stop()
	}

	if out == nil {
		return nil
	}

	if out.enc != nil {
		out.enc.Close()
	}

	out.closeSound()
	if pub == nil {
		return nil
	}

	s.pubMu.Lock()
	err := errors.Join(pub.RemoveTrack(out.tr.Sender()), pub.RemoveTrack(out.soundTr.Sender()))
	s.pubMu.Unlock()

	if err != nil {
		return err
	}

	return s.negotiate()
}

// Sharing says whether my screen is being published.
func (s *Session) Sharing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.vid.screenOn
}

// ScreenOptions bound what a capture sends: the largest size (fitting the source, never above it) and the rate.
type ScreenOptions struct {
	MaxWidth, MaxHeight, FPS int
	Window                   bool // the picker offers windows, else displays
	Sound                    bool // with its sound: a window's app, or everything a display plays (not this app)
}

// ErrPickerCancelled: the user closed the picker without choosing.
var ErrPickerCancelled = errors.New("compartilhamento cancelado")

// fitSize is the capture size: the source's pixels, scaled down (aspect kept) to fit max, in even numbers.
func fitSize(srcW, srcH, maxW, maxH int) (int, int) {
	if srcW <= 0 || srcH <= 0 {
		return maxW &^ 1, maxH &^ 1
	}

	scale := min(1, float64(maxW)/float64(srcW), float64(maxH)/float64(srcH))
	w, h := int(float64(srcW)*scale+0.5)&^1, int(float64(srcH)*scale+0.5)&^1

	return max(w, 2), max(h, 2)
}
