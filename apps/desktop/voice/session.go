package voice

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/kazzmir/opus-go/opus"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

// Status is where a call stands, as the dock shows it.
type Status uint8

const (
	Connecting Status = iota
	Connected
	Failed
	Ended // left, or moved to another call elsewhere
)

// The web's messages (features/voice/call.ts), so both apps say the same.
const (
	connectTimeout = 15 * time.Second
	gatherTimeout  = 3 * time.Second
	answerTimeout  = 10 * time.Second
	hangover       = 300 * time.Millisecond // keeps the speaking ring through the gaps between words

	msgNoServer   = "Sem conexão com o servidor."
	msgNoAnswer   = "O servidor não respondeu ao pedido de entrar na chamada."
	msgUDPBlocked = "Não conectou: a porta UDP de mídia do servidor pode estar bloqueada."
	msgMediaLost  = "A conexão de mídia caiu."
	msgServerLost = "A conexão com o servidor caiu."
	msgMoved      = "Você entrou em outra chamada em outra janela."
)

var voiceErrors = map[string]string{
	"forbidden": "Você não tem permissão para entrar nesta chamada.",
	"not found": "Esta chamada não existe mais.",
}

// Options say where to join and how. Input and Output are device IDs ("" for the default).
type Options struct {
	Server, Token, ChannelID string
	ListenOnly               bool   // no microphone: no Speak permission, or it was refused
	ListenOnlyReason         string // shown as the call's error when ListenOnly
	Input, Output            string
	Muted, Deafened          bool
	NoiseGate                bool // start with the noise gate on (SetNoiseGate changes it)
	Headless                 bool // no devices: the caller drives Capture and Mix (tests)
	// OnChange is called (from any goroutine, never blocking) when Status, Error or who speaks changes.
	OnChange func()
}

// Session is one call over the built-in SFU (docs/protocol.md, "Mídia (pion)"), on a gateway connection of its own:
// the server ties a call to the gateway session that joined it, so this one ends the call when it closes, and the
// app's main connection needs no way to send. Two peer connections: we offer on pub, the server offers on sub.
type Session struct {
	opt   Options
	mixer *Mixer

	gate   gate        // the microphone's noise gate: Capture's alone
	gateOn atomic.Bool // whether it runs

	ctx    context.Context
	cancel context.CancelFunc
	out    chan []byte // to the gateway, in order

	enc    *opus.Encoder
	mic    *webrtc.TrackLocalStaticSample
	api    *webrtc.API // receiving: every codec the server may forward
	pubAPI *webrtc.API // sending: Opus and our H.264 only, so the server picks them

	pubMu   sync.Mutex  // one negotiation of pub at a time: the next offer waits for the last answer
	answers chan string // RTC_ANSWER's SDPs, for the negotiation waiting on them

	mu            sync.Mutex
	pub, sub      *webrtc.PeerConnection
	pubNames      map[*webrtc.RTPTransceiver]string // what each of pub's transceivers sends: mic, screen
	vid           videoState
	pending       bool // voice_join sent, VOICE_READY not received
	micDone       bool
	listenOnly    bool // publishing nothing
	everConnected bool
	status        Status
	err           string
	me            string
	muted         bool
	deafened      bool
	micLevel      float32
	loudAt        map[string]time.Time // by user: the last frame above the speaking level
	speaking      map[string]bool
	speaker       io.Closer // the open output device
	micDev        io.Closer // the open input device
	closed        bool      // shut down: nothing opens any more
	encoded       []byte    // Capture's, on the input thread only
}

// Join starts a call and returns at once; Status follows it.
func Join(opt Options) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{opt: opt, mixer: NewMixer(), ctx: ctx, cancel: cancel, out: make(chan []byte, 64),
		listenOnly: opt.ListenOnly, muted: opt.Muted || opt.Deafened, deafened: opt.Deafened, loudAt: map[string]time.Time{}, speaking: map[string]bool{},
		encoded: make([]byte, 1500), answers: make(chan string, 1), pubNames: map[*webrtc.RTPTransceiver]string{},
		vid: videoState{frames: map[string]Picture{}, notes: map[string]string{}}}
	if opt.ListenOnly {
		s.err = opt.ListenOnlyReason
	}

	s.gate = newGate()
	s.gateOn.Store(opt.NoiseGate)

	s.mixer.SetDeafened(opt.Deafened)
	go s.run()

	return s
}

// Status is the call's state and its last error ("" when none).
func (s *Session) Status() (Status, string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.status, s.err
}

// SetNoiseGate turns the noise gate on or off (on by default, as the web's).
func (s *Session) SetNoiseGate(on bool) { s.gateOn.Store(on) }

// Speaking says whether a user (me included) is speaking now.
func (s *Session) Speaking(userID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.speaking[userID]
}

// ListenOnly says that my microphone is not in the call (no permission, or it could not be opened).
func (s *Session) ListenOnly() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.listenOnly
}

// MicLevel is my microphone's level, 0 to 1, muted or not (the settings' meter).
func (s *Session) MicLevel() float32 {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.micLevel
}

// SetVolume sets how loud a user plays, 0 to 2.
func (s *Session) SetVolume(userID string, v float32) { s.mixer.SetVolume(userID, v) }

// Move tells the others where I stand in a world (world_move; the server takes at most 25 a second).
func (s *Session) Move(x, y float32) { s.send("world_move", map[string]float32{"x": x, "y": y}) }

// WearSkin dresses me in a world skin, or in my own character with "" (world_skin).
func (s *Session) WearSkin(id string) { s.send("world_skin", map[string]string{"skin": id}) }

// React floats an emoji over me for the others in my room (world_react).
func (s *Session) React(emoji string) { s.send("world_react", map[string]string{"emoji": emoji}) }

// SetMuted applies my mute and deafen choices and tells the others (voice_state; deafened implies muted).
func (s *Session) SetMuted(muted, deafened bool) {
	s.mu.Lock()
	s.muted, s.deafened = muted || deafened, deafened
	effective := s.muted || s.listenOnly
	s.mu.Unlock()

	s.mixer.SetDeafened(deafened)
	s.send("voice_state", map[string]bool{"muted": effective, "deafened": deafened})
}

// Leave hangs up. The session is done after it.
func (s *Session) Leave() {
	s.mu.Lock()
	if s.status != Failed {
		s.status = Ended
	}
	s.mu.Unlock()

	s.send("voice_leave", nil)
	s.shutdown()
}

// Mix fills out (Frame samples) with everyone else; the output device calls it every 20 ms.
func (s *Session) Mix(out []int16) { s.mixer.Mix(out) }

// Capture takes 20 ms of my microphone: it measures it and, unmuted, sends it.
func (s *Session) Capture(pcm []int16) {
	level := Level(pcm)

	s.mu.Lock()
	s.micLevel = level
	enc, mic := s.enc, s.mic
	send := !s.muted && mic != nil && enc != nil
	if send && level*32768 > speakingRMS {
		s.loudAt[s.me] = time.Now()
	}
	s.mu.Unlock()

	if !send {
		return // muted: nothing goes out, as a disabled browser track (the others' buffers play silence)
	}

	if s.gateOn.Load() {
		s.gate.apply(pcm, level) // Capture runs on the input's one thread
	}

	n, err := enc.Encode(pcm, Frame, s.encoded)
	if err != nil {
		return
	}

	_ = mic.WriteSample(media.Sample{Data: s.encoded[:n], Duration: 20 * time.Millisecond})
}

func (s *Session) send(op string, d any) {
	b, err := json.Marshal(map[string]any{"op": op, "d": d})
	if err != nil {
		return
	}

	select {
	case s.out <- b:
	case <-s.ctx.Done():
	}
}

func (s *Session) changed() {
	if s.opt.OnChange != nil {
		s.opt.OnChange()
	}
}

// fail ends the call with an error the dock shows.
func (s *Session) fail(msg string) {
	s.mu.Lock()
	if s.status == Failed || s.status == Ended {
		s.mu.Unlock()
		return
	}

	s.status, s.err = Failed, msg
	s.mu.Unlock()

	s.send("voice_leave", nil)
	s.shutdown()
	s.changed()
}

// shutdown closes the devices, the peer connections and (after what is queued is written) the gateway.
func (s *Session) shutdown() {
	s.mu.Lock()
	devices := []io.Closer{s.speaker, s.micDev}
	s.speaker, s.micDev, s.closed = nil, nil, true
	s.mu.Unlock()

	for _, d := range devices {
		if d != nil {
			d.Close()
		}
	}

	go func() {
		time.Sleep(200 * time.Millisecond) // lets the writer flush voice_leave
		s.cancel()
	}()
}

func (s *Session) setError(msg string) {
	s.mu.Lock()
	s.err = msg
	s.mu.Unlock()
	s.changed()
}

func (s *Session) run() {
	defer s.closePeers()

	url := "ws" + strings.TrimPrefix(s.opt.Server, "http") + "/gateway"
	dialCtx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	ws, _, err := websocket.Dial(dialCtx, url, nil)
	cancel()
	if err != nil {
		s.fail(msgNoServer)
		return
	}
	defer ws.CloseNow()

	ws.SetReadLimit(8 << 20) // READY carries every guild

	hello, _ := json.Marshal(map[string]string{"token": s.opt.Token})
	if ws.Write(s.ctx, websocket.MessageText, hello) != nil {
		s.fail(msgNoServer)
		return
	}

	go func() {
		for {
			select {
			case b := <-s.out:
				if ws.Write(s.ctx, websocket.MessageText, b) != nil {
					return
				}
			case <-s.ctx.Done():
				ws.Close(websocket.StatusNormalClosure, "")
				return
			}
		}
	}()

	s.mu.Lock()
	s.pending = true
	s.mu.Unlock()
	s.send("voice_join", map[string]string{"channel_id": s.opt.ChannelID})

	timer := time.AfterFunc(connectTimeout, func() {
		s.mu.Lock()
		connecting, pending := s.status == Connecting, s.pending
		s.mu.Unlock()

		switch {
		case connecting && pending:
			s.fail(msgNoAnswer)
		case connecting:
			s.fail(msgUDPBlocked)
		}
	})
	defer timer.Stop()

	go s.watchSpeaking()

	for {
		_, data, err := ws.Read(s.ctx)
		if err != nil {
			if s.ctx.Err() == nil {
				if websocket.CloseStatus(err) == websocket.StatusPolicyViolation {
					s.fail("Sua sessão expirou. Entre de novo.")
				} else {
					s.fail(msgServerLost)
				}
			}

			return
		}

		var e struct {
			T string          `json:"t"`
			D json.RawMessage `json:"d"`
		}
		if json.Unmarshal(data, &e) != nil {
			continue
		}

		s.handle(e.T, e.D)
	}
}

// handle processes one gateway event; only this goroutine negotiates, so offers and answers never interleave.
func (s *Session) handle(t string, d json.RawMessage) {
	switch t {
	case "READY":
		var r struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
		}
		json.Unmarshal(d, &r)
		s.mu.Lock()
		s.me = r.User.ID
		s.mu.Unlock()

	case "VOICE_READY":
		var r struct {
			ChannelID  string `json:"channel_id"`
			Connection struct {
				Kind string `json:"kind"`
			} `json:"connection"`
		}
		if json.Unmarshal(d, &r) != nil || r.ChannelID != s.opt.ChannelID {
			return
		}

		s.mu.Lock()
		s.pending = false
		s.mu.Unlock()

		if r.Connection.Kind != "pion" {
			s.fail(`Este app não suporta o tipo de conexão de mídia do servidor ("` + r.Connection.Kind + `").`)
			return
		}

		if err := s.startMedia(); err != nil {
			s.fail("Falha ao iniciar a mídia: " + err.Error())
		}

	case "VOICE_ERROR":
		var r struct{ Op, Error string }
		json.Unmarshal(d, &r)
		text := r.Error
		if v, ok := voiceErrors[text]; ok {
			text = v
		}

		switch r.Op {
		case "voice_join", "rtc_offer", "rtc_answer":
			s.fail("Erro na chamada: " + text)
		case "voice_state":
		default:
			s.setError("Erro na chamada: " + text)
		}

	case "RTC_ANSWER":
		var r struct{ SDP string }
		if json.Unmarshal(d, &r) != nil {
			return
		}

		select {
		case s.answers <- r.SDP:
		default: // nobody waits for it: a late answer to an offer that timed out
		}

	case "RTC_OFFER":
		var r struct{ SDP string }
		if json.Unmarshal(d, &r) != nil {
			return
		}

		if err := s.answer(r.SDP); err != nil {
			s.fail("Falha ao negociar a recepção: " + err.Error())
		}

	case "VOICE_STATE_UPDATE":
		var v struct {
			UserID    string `json:"user_id"`
			ChannelID string `json:"channel_id"`
		}
		json.Unmarshal(d, &v)
		s.mu.Lock()
		moved := v.UserID == s.me && s.me != "" && v.ChannelID != "" && v.ChannelID != s.opt.ChannelID && !s.pending
		s.mu.Unlock()

		if moved {
			s.mu.Lock()
			s.status, s.err = Ended, msgMoved
			s.mu.Unlock()
			s.shutdown()
			s.changed()
		}
	}
}

// newAPI makes a WebRTC API: with every default codec to receive, or with only what we send.
func newAPI(receive bool) (*webrtc.API, error) {
	m := &webrtc.MediaEngine{}
	if receive {
		if err := m.RegisterDefaultCodecs(); err != nil {
			return nil, err
		}
	} else {
		for _, c := range []struct {
			cap  webrtc.RTPCodecCapability
			pt   webrtc.PayloadType
			kind webrtc.RTPCodecType
		}{
			{webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: Rate, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1"}, 111, webrtc.RTPCodecTypeAudio},
			{h264, 102, webrtc.RTPCodecTypeVideo},
		} {
			if err := m.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: c.cap, PayloadType: c.pt}, c.kind); err != nil {
				return nil, err
			}
		}
	}

	// Default interceptors: NACK (retransmission), RTCP reports and transport-wide congestion feedback, as the server.
	ir := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, ir); err != nil {
		return nil, err
	}

	return webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(ir)), nil
}

func (s *Session) peer(api *webrtc.API) (*webrtc.PeerConnection, error) {
	// The server's SDPs carry its own candidates (public IP, one UDP port): no STUN or TURN needed.
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, err
	}

	pc.OnConnectionStateChange(func(webrtc.PeerConnectionState) { s.sync() })

	return pc, nil
}

// startMedia opens the speakers and, unless listening only, the microphone and the publish connection.
func (s *Session) startMedia() error {
	var err error
	if s.api, err = newAPI(true); err != nil {
		return err
	}

	if s.pubAPI, err = newAPI(false); err != nil {
		return err
	}

	if !s.opt.Headless {
		if err := s.openDevice(false); err != nil {
			return errors.New("alto-falante: " + err.Error())
		}
	}

	if !s.opt.ListenOnly {
		if err := s.publishMic(); err != nil {
			s.mu.Lock()
			s.listenOnly = true
			s.mu.Unlock()
			s.setError(err.Error() + " Você está só ouvindo.")
		}
	}

	s.mu.Lock()
	muted, deafened := s.muted || s.listenOnly, s.deafened
	s.mu.Unlock()
	s.send("voice_state", map[string]bool{"muted": muted, "deafened": deafened})

	s.mu.Lock()
	s.micDone = true
	s.mu.Unlock()
	s.sync()

	return nil
}

func (s *Session) publishMic() error {
	enc, err := opus.NewEncoder(Rate, 1, opus.ApplicationVoIP)
	if err != nil {
		return err
	}

	enc.SetBitrate(48000)

	track, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: Rate, Channels: 2}, "mic", "discordo")
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

	go drainRTCP(tr.Sender())

	s.mu.Lock()
	s.pubNames[tr] = "mic"
	s.enc, s.mic = enc, track
	s.mu.Unlock()
	s.pubMu.Unlock()

	// In the background: the answer comes through this goroutine (the reader), which must not wait for it.
	go s.negotiate()

	if s.opt.Headless {
		return nil
	}

	if err := s.openDevice(true); err != nil {
		return errors.New("Não foi possível abrir o microfone: " + err.Error() + ".")
	}

	return nil
}

// ensurePub is the publish connection, made on first use. The caller holds pubMu.
func (s *Session) ensurePub() (*webrtc.PeerConnection, error) {
	s.mu.Lock()
	pub, api := s.pub, s.pubAPI
	s.mu.Unlock()

	if pub != nil {
		return pub, nil
	}

	if api == nil {
		return nil, errors.New("a chamada ainda não está pronta")
	}

	pub, err := s.peer(api)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.pub = pub
	s.mu.Unlock()

	return pub, nil
}

// negotiate offers pub's current tracks to the server and applies its answer. It returns once the answer is in.
func (s *Session) negotiate() error {
	s.pubMu.Lock()
	defer s.pubMu.Unlock()

	s.mu.Lock()
	pub := s.pub
	s.mu.Unlock()

	if pub == nil {
		return nil
	}

	offer, err := pub.CreateOffer(nil)
	if err == nil {
		err = setLocal(pub, offer)
	}

	if err != nil {
		s.setError("Falha ao negociar o envio: " + err.Error())
		return err
	}

	tracks := map[string]string{}
	s.mu.Lock()
	for tr, name := range s.pubNames {
		tracks[tr.Mid()] = name
	}
	s.mu.Unlock()

	select {
	case <-s.answers: // a stale one
	default:
	}

	s.send("rtc_offer", map[string]any{"sdp": pub.LocalDescription().SDP, "tracks": tracks})

	select {
	case sdp := <-s.answers:
		if err := pub.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sdp}); err != nil {
			s.fail("Falha ao negociar o envio: " + err.Error())
			return err
		}

		s.sync()

		return nil

	case <-time.After(answerTimeout):
		err := errors.New("o servidor não respondeu à oferta de mídia")
		s.setError("Falha ao negociar o envio: " + err.Error())
		return err

	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

// openDevice opens the chosen microphone or speaker in place of the open one.
func (s *Session) openDevice(input bool) error {
	s.mu.Lock()
	id := s.opt.Output
	if input {
		id = s.opt.Input
	}
	s.mu.Unlock()

	var dev io.Closer
	var err error
	if input {
		dev, err = OpenInput(id, s.Capture)
	} else {
		dev, err = OpenOutput(id, s.mixer.Mix)
	}

	if err != nil {
		return err
	}

	s.mu.Lock()
	slot := &s.speaker
	if input {
		slot = &s.micDev
	}

	old, closed := *slot, s.closed
	if !closed {
		*slot = dev
	}
	s.mu.Unlock()

	if old != nil {
		old.Close()
	}

	if closed {
		dev.Close()
	}

	return nil
}

// SetDevices moves the call to other devices ("" for the default). It blocks while they open: not on the UI thread.
func (s *Session) SetDevices(input, output string) error {
	s.mu.Lock()
	s.opt.Input, s.opt.Output = input, output
	speaker, mic := s.speaker != nil, s.micDev != nil
	s.mu.Unlock()

	var errs []error
	if speaker {
		errs = append(errs, s.openDevice(false))
	}

	if mic {
		errs = append(errs, s.openDevice(true))
	}

	return errors.Join(errs...)
}

// answer applies the server's offer for what we receive.
func (s *Session) answer(sdp string) error {
	s.mu.Lock()
	sub := s.sub
	s.mu.Unlock()

	if sub == nil {
		if s.api == nil {
			return errors.New("oferta antes de VOICE_READY")
		}

		var err error
		if sub, err = s.peer(s.api); err != nil {
			return err
		}

		sub.OnTrack(s.receive)
		s.mu.Lock()
		s.sub = sub
		s.mu.Unlock()
	}

	if err := sub.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sdp}); err != nil {
		return err
	}

	answer, err := sub.CreateAnswer(nil)
	if err == nil {
		err = setLocal(sub, answer)
	}

	if err != nil {
		return err
	}

	s.send("rtc_answer", map[string]string{"sdp": sub.LocalDescription().SDP})
	s.sync()

	return nil
}

// receive runs for the life of one remote track. Its stream ID is the owner's user ID, its ID the track's name.
func (s *Session) receive(remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	go drainRTCP(receiver)

	if remote.Kind() != webrtc.RTPCodecTypeAudio {
		s.receiveVideo(remote) // only after rtc_subscribe (Watch)
		return
	}

	src, err := s.mixer.Add(remote.StreamID())
	if err != nil {
		return
	}
	defer s.mixer.Remove(src)

	for {
		pkt, _, err := remote.ReadRTP()
		if err != nil {
			return
		}

		if len(pkt.Payload) > 0 {
			s.mixer.Push(src, pkt.SequenceNumber, pkt.Payload)
		}
	}
}

// sync reflects the peer connections once the microphone is set up (so we never flash "connected" before publishing).
func (s *Session) sync() {
	s.mu.Lock()
	ready, pcs, ever := s.micDone, []*webrtc.PeerConnection{s.pub, s.sub}, s.everConnected
	s.mu.Unlock()

	if !ready {
		return
	}

	failed, connected := false, true
	for _, pc := range pcs {
		if pc == nil {
			continue
		}

		switch pc.ConnectionState() {
		case webrtc.PeerConnectionStateFailed:
			failed = true
		case webrtc.PeerConnectionStateConnected:
		default:
			connected = false
		}
	}

	switch {
	case failed && ever:
		s.fail(msgMediaLost)
	case failed:
		s.fail(msgUDPBlocked)
	case connected:
		s.mu.Lock()
		s.everConnected = true
		change := s.status == Connecting
		if change {
			s.status = Connected
		}
		s.mu.Unlock()

		if change {
			s.changed()
		}
	}
}

// watchSpeaking turns the levels into who is speaking, and tells the app when that changes.
func (s *Session) watchSpeaking() {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()

	loud := map[string]bool{}
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-tick.C:
		}

		clear(loud)
		s.mixer.Speaking(loud)
		now := time.Now()

		s.mu.Lock()
		for u := range loud {
			s.loudAt[u] = now
		}

		change := false
		for u, at := range s.loudAt {
			on := now.Sub(at) < hangover && !(u == s.me && s.muted)
			if on != s.speaking[u] {
				s.speaking[u], change = on, true
			}
		}
		s.mu.Unlock()

		if change {
			s.changed()
		}
	}
}

func (s *Session) closePeers() {
	s.mu.Lock()
	pcs, enc := []*webrtc.PeerConnection{s.pub, s.sub}, s.enc
	s.mic, s.enc = nil, nil
	screen, capture := s.vid.screen, s.vid.capture
	s.vid.screen, s.vid.screenOn, s.vid.capture = nil, false, nil
	if screen != nil {
		screen.lastPic.Release()
		screen.lastPic = 0
	}

	for k, p := range s.vid.frames {
		p.Release()
		delete(s.vid.frames, k)
	}
	s.mu.Unlock()

	if screen != nil && screen.enc != nil {
		screen.enc.Close()
	}

	if screen != nil {
		screen.closeSound()
	}

	if capture != nil {
		capture.Stop()
	}

	for _, pc := range pcs {
		if pc != nil {
			pc.Close()
		}
	}

	if enc != nil {
		enc.Close()
	}
}

// setLocal applies a local description and waits for its candidates: there is no trickle ICE.
func setLocal(pc *webrtc.PeerConnection, desc webrtc.SessionDescription) error {
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(desc); err != nil {
		return err
	}

	select {
	case <-gathered:
	case <-time.After(gatherTimeout): // goes with what it has, as the web does
	}

	return nil
}

// drainRTCP reads a sender's or receiver's RTCP, which runs the interceptors (NACK, reports).
func drainRTCP(r interface {
	Read([]byte) (int, interceptor.Attributes, error)
}) {
	buf := make([]byte, 1500)
	for {
		if _, _, err := r.Read(buf); err != nil {
			return
		}
	}
}
