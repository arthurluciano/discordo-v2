// Package pion is the built-in SFU: it forwards each participant's RTP to the others without transcoding.
//
// Every participant has two peer connections, so offers never collide:
//   - publish: the client offers ("rtc_offer") whenever what it sends changes; we answer ("RTC_ANSWER").
//   - subscribe: we offer ("RTC_OFFER") whenever what it receives changes; the client answers ("rtc_answer").
//
// There is no trickle ICE: SDPs are sent with all candidates in them (we only have host candidates on one UDP port).
// Audio reaches everyone in the room. Video is forwarded only to those who ask ("rtc_subscribe"),
// so a 4K stream costs bandwidth only for the people watching it.
//
// Video may be simulcast: the publisher sends up to three layers of the same track (rids "h", "m", "l"),
// and each subscriber gets one of them (layer.go), switched on keyframes with sequence numbers and
// timestamps rewritten so the subscriber sees a single continuous stream.
package pion

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"discordo/server/internal/errs"
	"discordo/server/internal/media"
)

type Config struct {
	UDPPort  int    // all media flows through this one port; 0 picks a free one
	PublicIP string // the address clients reach us at; empty means local development (loopback candidates included)
}

// SFU implements media.Provider.
// shortcut: one lock for all rooms, held while negotiating; give each room its own when contention shows up.
type SFU struct {
	api      *webrtc.API
	conn     net.PacketConn
	onTracks func(roomID, userID string, tracks []string)

	mu    sync.Mutex
	rooms map[string]*room
}

type room struct {
	peers  map[string]*peer
	tracks map[string]*track // key: userID + "/" + track name
}

type peer struct {
	userID string
	grants media.Grants
	send   media.Sender
	pub    *webrtc.PeerConnection // created on the first rtc_offer
	sub    *webrtc.PeerConnection
	names  map[string]string // publish transceiver mid -> track name, from the client's offers
	sends  map[string]*down  // track key -> what we forward to this peer
	wants  map[string]string // video track keys this peer asked for -> the quality it asked for
	dirty  bool              // subscriptions changed while an offer was in flight
	closed bool
}

// track is one published track. With simulcast, each of its layers arrives as its own TrackRemote.
type track struct {
	owner    *peer
	name     string
	kind     webrtc.RTPCodecType
	codec    webrtc.RTPCodecCapability
	receiver *webrtc.RTPReceiver
	pub      *webrtc.PeerConnection // the owner's, for keyframe requests

	mu     sync.Mutex                     // guards the fields below; taken after SFU.mu when both are held
	layers map[string]*webrtc.TrackRemote // by rid; "" without simulcast
	downs  map[*peer]*down
	lastKF map[string]time.Time // by rid: last keyframe request, to throttle them
}

// down is one track as forwarded to one subscriber.
type down struct {
	t       *track
	local   *webrtc.TrackLocalStaticRTP
	sender  *webrtc.RTPSender
	quality string // auto, high, medium or low
	auto    autoQuality
	cur     string // the layer being forwarded, once on is set
	on      bool
	want    string // the layer to forward from its next keyframe on
	rw      rewriter
}

func New(cfg Config) (*SFU, error) {
	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		return nil, err
	}
	// Default interceptors: NACK (retransmission), RTCP reports and transport-wide congestion feedback.
	ir := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, ir); err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: cfg.UDPPort})
	if err != nil {
		return nil, err
	}
	se := webrtc.SettingEngine{}
	se.SetICEUDPMux(webrtc.NewICEUDPMux(nil, conn))
	if cfg.PublicIP != "" {
		se.SetNAT1To1IPs([]string{cfg.PublicIP}, webrtc.ICECandidateTypeHost)
	} else {
		se.SetIncludeLoopbackCandidate(true)
	}
	return &SFU{
		api:      webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(ir), webrtc.WithSettingEngine(se)),
		conn:     conn,
		onTracks: func(string, string, []string) {},
		rooms:    map[string]*room{},
	}, nil
}

func (s *SFU) Close() error { return s.conn.Close() }

func (s *SFU) OnTracks(fn func(roomID, userID string, tracks []string)) { s.onTracks = fn }

func (s *SFU) Join(_ context.Context, roomID, userID string, grants media.Grants, send media.Sender) (media.Connection, error) {
	sub, err := s.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return media.Connection{}, err
	}
	p := &peer{userID: userID, grants: grants, send: send, sub: sub, sends: map[string]*down{}, wants: map[string]string{}}

	s.mu.Lock()
	r := s.rooms[roomID]
	if r == nil {
		r = &room{peers: map[string]*peer{}, tracks: map[string]*track{}}
		s.rooms[roomID] = r
	}
	var stale []*webrtc.PeerConnection
	if old := r.peers[userID]; old != nil {
		stale = s.removeLocked(roomID, r, old)
	}
	s.rooms[roomID] = r // removeLocked may have dropped the room when the old peer was its only one
	r.peers[userID] = p
	for key, t := range r.tracks {
		if t.kind == webrtc.RTPCodecTypeAudio {
			s.attachLocked(p, key, t)
		}
	}
	s.negotiateLocked(p)
	s.mu.Unlock()

	closeAll(stale)
	return media.Connection{Kind: "pion"}, nil
}

func (s *SFU) Leave(roomID, userID string) {
	s.mu.Lock()
	var stale []*webrtc.PeerConnection
	if r := s.rooms[roomID]; r != nil {
		if p := r.peers[userID]; p != nil {
			stale = s.removeLocked(roomID, r, p)
		}
	}
	s.mu.Unlock()
	closeAll(stale)
}

func (s *SFU) Signal(_ context.Context, roomID, userID, typ string, data json.RawMessage) error {
	var msg struct {
		SDP     string            `json:"sdp"`
		Tracks  map[string]string `json:"tracks"` // rtc_offer: mid -> track name
		UserID  string            `json:"user_id"`
		Track   string            `json:"track"`
		On      bool              `json:"on"`
		Quality string            `json:"quality"` // rtc_subscribe: auto (default), high, medium or low
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return errs.Invalid("malformed signaling message")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rooms[roomID]
	if r == nil || r.peers[userID] == nil {
		return errs.ErrNotFound
	}
	p := r.peers[userID]

	switch typ {
	case "rtc_offer":
		p.names = msg.Tracks
		return s.answerLocked(roomID, p, msg.SDP)
	case "rtc_answer":
		if err := p.sub.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: msg.SDP}); err != nil {
			return errs.Invalid("bad answer: " + err.Error())
		}
		if p.dirty {
			p.dirty = false
			s.negotiateLocked(p)
		}
		return nil
	case "rtc_subscribe":
		if msg.Track != media.Camera && msg.Track != media.Screen {
			return errs.Invalid("only camera and screen are subscribed on demand")
		}
		if msg.Quality == "" {
			msg.Quality = "auto"
		}
		if _, ok := qualityLevel[msg.Quality]; !ok {
			return errs.Invalid("quality must be auto, high, medium or low")
		}
		key := msg.UserID + "/" + msg.Track
		if !msg.On {
			delete(p.wants, key)
			if p.sends[key] != nil {
				detach(p, key)
				s.negotiateLocked(p)
			}
			return nil
		}
		p.wants[key] = msg.Quality
		if d := p.sends[key]; d != nil { // already watching: only the layer changes, no renegotiation
			d.t.mu.Lock()
			d.quality = msg.Quality
			d.t.retargetLocked(d)
			d.t.mu.Unlock()
			return nil
		}
		if t := r.tracks[key]; t != nil && t.owner != p {
			s.attachLocked(p, key, t)
			s.negotiateLocked(p)
		}
		return nil
	}
	return errs.Invalid("unknown signaling message")
}

// answerLocked applies the client's publish offer.
func (s *SFU) answerLocked(roomID string, p *peer, sdp string) error {
	if p.pub == nil {
		pub, err := s.api.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			return err
		}
		pub.OnTrack(func(remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) { s.forward(roomID, p, remote, receiver) })
		p.pub = pub
	}
	if err := p.pub.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sdp}); err != nil {
		return errs.Invalid("bad offer: " + err.Error())
	}
	answer, err := p.pub.CreateAnswer(nil)
	if err != nil {
		return err
	}
	if err := setLocal(p.pub, answer); err != nil {
		return err
	}
	p.send("RTC_ANSWER", map[string]string{"sdp": p.pub.LocalDescription().SDP})
	return nil
}

// negotiateLocked offers the peer its current set of subscriptions.
func (s *SFU) negotiateLocked(p *peer) {
	if p.closed || len(p.sub.GetTransceivers()) == 0 {
		return // nothing to receive yet
	}
	if p.sub.SignalingState() != webrtc.SignalingStateStable {
		p.dirty = true // an offer is in flight; redo when its answer arrives
		return
	}
	offer, err := p.sub.CreateOffer(nil)
	if err == nil {
		err = setLocal(p.sub, offer)
	}
	if err != nil {
		slog.Warn("sfu: creating subscribe offer", "user", p.userID, "err", err)
		return
	}
	p.send("RTC_OFFER", map[string]string{"sdp": p.sub.LocalDescription().SDP})
}

// setLocal applies a local description and waits until its candidates are in it.
func setLocal(pc *webrtc.PeerConnection, desc webrtc.SessionDescription) error {
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(desc); err != nil {
		return err
	}
	select {
	case <-gathered:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("ICE gathering timed out")
	}
}

// forward runs for the lifetime of one published track.
func (s *SFU) forward(roomID string, p *peer, remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	s.mu.Lock()
	name, audio := p.trackName(remote, receiver), remote.Kind() == webrtc.RTPCodecTypeAudio
	s.mu.Unlock()
	var allowed bool
	switch name {
	case media.Mic:
		allowed = audio && p.grants.Speak
	case media.ScreenAudio:
		allowed = audio && p.grants.Stream
	case media.Camera, media.Screen:
		allowed = !audio && p.grants.Stream
	}
	if !allowed {
		return // unknown name or no permission: the track is simply never forwarded
	}
	key, rid := p.userID+"/"+name, remote.RID()

	s.mu.Lock()
	r := s.rooms[roomID]
	if p.closed || r == nil {
		s.mu.Unlock()
		return
	}
	t := r.tracks[key]
	switch {
	case t == nil: // the track's first layer (or its only one)
		t = &track{owner: p, name: name, kind: remote.Kind(), codec: remote.Codec().RTPCodecCapability, receiver: receiver, pub: p.pub,
			layers: map[string]*webrtc.TrackRemote{rid: remote}, downs: map[*peer]*down{}, lastKF: map[string]time.Time{}}
		r.tracks[key] = t
		for _, other := range r.peers {
			if other == p {
				continue
			}
			if _, wants := other.wants[key]; audio || wants {
				s.attachLocked(other, key, t)
				s.negotiateLocked(other)
			}
		}
		published := r.published(p.userID)
		s.mu.Unlock()
		s.onTracks(roomID, p.userID, published)
	case t.receiver == receiver: // another simulcast layer of it
		t.mu.Lock()
		t.layers[rid] = remote
		for _, d := range t.downs {
			t.retargetLocked(d)
		}
		t.mu.Unlock()
		s.mu.Unlock()
	default: // a second track with the same name
		s.mu.Unlock()
		return
	}

	for {
		pkt, _, err := remote.ReadRTP()
		if err != nil {
			break // the publisher stopped the track or left
		}
		t.write(rid, pkt)
	}

	s.mu.Lock()
	t.mu.Lock()
	delete(t.layers, rid)
	gone := len(t.layers) == 0
	for _, d := range t.downs {
		t.retargetLocked(d)
	}
	t.mu.Unlock()
	if r := s.rooms[roomID]; gone && r != nil && r.tracks[key] == t {
		s.unpublishLocked(r, key)
		published := r.published(p.userID)
		s.mu.Unlock()
		s.onTracks(roomID, p.userID, published)
		return
	}
	s.mu.Unlock()
}

// write forwards one packet of a layer to the subscribers on it, and moves those waiting for this layer onto it at a keyframe.
func (t *track) write(rid string, pkt *rtp.Packet) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now, checked, key := time.Now(), false, false
	for _, d := range t.downs {
		if d.want == rid && (!d.on || d.cur != rid) {
			if !checked {
				checked, key = true, t.kind == webrtc.RTPCodecTypeAudio || isKeyframe(t.codec.MimeType, pkt.Payload)
			}
			// Starting can happen anywhere (the decoder waits for the keyframe we asked for); a switch waits for
			// one, or the decoder would apply the new layer's deltas to the old layer's pictures.
			if key || !d.on {
				d.cur, d.on = rid, true
				d.rw.switchTo(pkt.SequenceNumber, pkt.Timestamp, t.codec.ClockRate, now)
			}
		}
		if !d.on || d.cur != rid {
			continue
		}
		out := *pkt
		out.SequenceNumber, out.Timestamp = d.rw.rewrite(pkt.SequenceNumber, pkt.Timestamp, now)
		// The publisher's header extension IDs mean nothing on the subscriber's connection (and its mid/rid would misroute);
		// the subscriber connection's interceptors add their own.
		out.Extension, out.Extensions = false, nil
		_ = d.local.WriteRTP(&out) // a subscriber's failing connection must not stop the others
	}
}

// trackName is what the client called a track in its offer ("tracks", by mid). Browsers cannot
// choose their track IDs, so they name tracks that way; other clients may just use the msid track ID.
func (p *peer) trackName(remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) string {
	for _, tr := range p.pub.GetTransceivers() {
		if tr.Receiver() == receiver {
			if name, ok := p.names[tr.Mid()]; ok {
				return name
			}
		}
	}
	return remote.ID()
}

func (r *room) published(userID string) []string {
	names := []string{}
	for _, t := range r.tracks {
		if t.owner.userID == userID {
			names = append(names, t.name)
		}
	}
	slices.Sort(names)
	return names
}

// attachLocked starts forwarding a track to a peer. The caller renegotiates.
func (s *SFU) attachLocked(p *peer, key string, t *track) {
	if p.sends[key] != nil {
		return
	}
	local, err := webrtc.NewTrackLocalStaticRTP(t.codec, t.name, t.owner.userID)
	if err != nil {
		slog.Warn("sfu: creating forward track", "err", err)
		return
	}
	sender, err := p.sub.AddTrack(local)
	if err != nil {
		slog.Warn("sfu: adding track", "err", err)
		return
	}
	d := &down{t: t, local: local, sender: sender, quality: p.wants[key]}
	p.sends[key] = d
	t.mu.Lock()
	t.downs[p] = d
	t.retargetLocked(d) // for video, also asks for the keyframe a new viewer needs to start decoding
	t.mu.Unlock()
	// RTCP from the viewer: a lost picture becomes a keyframe request to the publisher, and loss drives "auto" quality.
	go func() {
		for {
			pkts, _, err := sender.ReadRTCP()
			if err != nil {
				return
			}
			t.mu.Lock()
			for _, pkt := range pkts {
				switch pkt := pkt.(type) {
				case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
					t.keyframeLocked(d.want)
				case *rtcp.ReceiverReport:
					for _, rep := range pkt.Reports {
						if d.quality == "auto" && d.auto.report(rep.FractionLost) {
							t.retargetLocked(d)
						}
					}
				}
			}
			t.mu.Unlock()
		}
	}()
}

// retargetLocked picks the layer a subscriber should get and, if it changes, asks for a keyframe on it to switch.
func (t *track) retargetLocked(d *down) {
	if len(t.layers) == 0 {
		return
	}
	level := qualityLevel[d.quality]
	if d.quality == "auto" {
		level = d.auto.level
	}
	d.want = pickLayer(t.layers, level)
	if t.kind == webrtc.RTPCodecTypeVideo && (!d.on || d.cur != d.want) {
		t.keyframeLocked(d.want)
	}
}

func detach(p *peer, key string) {
	d := p.sends[key]
	if d == nil {
		return
	}
	d.t.mu.Lock()
	delete(d.t.downs, p)
	d.t.mu.Unlock()
	if err := p.sub.RemoveTrack(d.sender); err != nil {
		slog.Warn("sfu: removing track", "err", err)
	}
	delete(p.sends, key)
}

func (t *track) keyframeLocked(rid string) {
	layer := t.layers[rid]
	if t.pub == nil || layer == nil || time.Since(t.lastKF[rid]) < 300*time.Millisecond {
		return
	}
	t.lastKF[rid] = time.Now()
	_ = t.pub.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(layer.SSRC())}})
}

// unpublishLocked stops forwarding a track to everyone.
func (s *SFU) unpublishLocked(r *room, key string) {
	delete(r.tracks, key)
	for _, other := range r.peers {
		if other.sends[key] != nil {
			detach(other, key)
			s.negotiateLocked(other)
		}
	}
}

// removeLocked takes a peer out of its room and returns its connections for the caller to close outside the lock.
func (s *SFU) removeLocked(roomID string, r *room, p *peer) []*webrtc.PeerConnection {
	p.closed = true
	delete(r.peers, p.userID)
	for key, t := range r.tracks {
		if t.owner == p {
			s.unpublishLocked(r, key)
		}
	}
	if len(r.peers) == 0 {
		delete(s.rooms, roomID)
	}
	return []*webrtc.PeerConnection{p.pub, p.sub}
}

func closeAll(pcs []*webrtc.PeerConnection) {
	for _, pc := range pcs {
		if pc != nil {
			_ = pc.Close()
		}
	}
}
