// Package pion is the built-in SFU: it forwards each participant's RTP to the others without transcoding.
//
// Every participant has two peer connections, so offers never collide:
//   - publish: the client offers ("rtc_offer") whenever what it sends changes; we answer ("RTC_ANSWER").
//   - subscribe: we offer ("RTC_OFFER") whenever what it receives changes; the client answers ("rtc_answer").
//
// There is no trickle ICE: SDPs are sent with all candidates in them (we only have host candidates on one UDP port).
// Audio reaches everyone in the room. Video is forwarded only to those who ask ("rtc_subscribe"),
// so a 4K stream costs bandwidth only for the people watching it.
package pion

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
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
	sends  map[string]*webrtc.RTPSender // track key -> what we forward to this peer
	wants  map[string]bool              // video track keys this peer asked for
	dirty  bool                         // subscriptions changed while an offer was in flight
	closed bool
}

type track struct {
	owner  *peer
	name   string
	local  *webrtc.TrackLocalStaticRTP
	remote *webrtc.TrackRemote
	lastKF time.Time // last keyframe request, to throttle them
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
	p := &peer{userID: userID, grants: grants, send: send, sub: sub, sends: map[string]*webrtc.RTPSender{}, wants: map[string]bool{}}

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
		if t.remote.Kind() == webrtc.RTPCodecTypeAudio {
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
		SDP    string `json:"sdp"`
		UserID string `json:"user_id"`
		Track  string `json:"track"`
		On     bool   `json:"on"`
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
		key := msg.UserID + "/" + msg.Track
		if msg.On {
			p.wants[key] = true
			if t := r.tracks[key]; t != nil && t.owner != p {
				s.attachLocked(p, key, t)
			}
		} else {
			delete(p.wants, key)
			detach(p, key)
		}
		s.negotiateLocked(p)
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
		pub.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) { s.forward(roomID, p, remote) })
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
func (s *SFU) forward(roomID string, p *peer, remote *webrtc.TrackRemote) {
	name, audio := remote.ID(), remote.Kind() == webrtc.RTPCodecTypeAudio
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
	local, err := webrtc.NewTrackLocalStaticRTP(remote.Codec().RTPCodecCapability, name, p.userID)
	if err != nil {
		slog.Warn("sfu: creating forward track", "err", err)
		return
	}
	key := p.userID + "/" + name
	t := &track{owner: p, name: name, local: local, remote: remote}

	s.mu.Lock()
	r := s.rooms[roomID]
	if p.closed || r == nil || r.tracks[key] != nil {
		s.mu.Unlock()
		return
	}
	r.tracks[key] = t
	for _, other := range r.peers {
		if other != p && (audio || other.wants[key]) {
			s.attachLocked(other, key, t)
			s.negotiateLocked(other)
		}
	}
	published := r.published(p.userID)
	s.mu.Unlock()
	s.onTracks(roomID, p.userID, published)

	for {
		pkt, _, err := remote.ReadRTP()
		if err != nil {
			break // the publisher stopped the track or left
		}
		if err := local.WriteRTP(pkt); err != nil && !errors.Is(err, io.ErrClosedPipe) {
			break
		}
	}

	s.mu.Lock()
	if r := s.rooms[roomID]; r != nil && r.tracks[key] == t {
		s.unpublishLocked(r, key)
		published = r.published(p.userID)
		s.mu.Unlock()
		s.onTracks(roomID, p.userID, published)
		return
	}
	s.mu.Unlock()
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
	sender, err := p.sub.AddTrack(t.local)
	if err != nil {
		slog.Warn("sfu: adding track", "err", err)
		return
	}
	p.sends[key] = sender
	if t.remote.Kind() == webrtc.RTPCodecTypeVideo {
		s.keyframeLocked(t) // a new viewer cannot decode until the next keyframe
	}
	// RTCP from the viewer: a lost-picture report becomes a keyframe request to the publisher.
	go func() {
		for {
			pkts, _, err := sender.ReadRTCP()
			if err != nil {
				return
			}
			for _, pkt := range pkts {
				switch pkt.(type) {
				case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
					s.mu.Lock()
					s.keyframeLocked(t)
					s.mu.Unlock()
				}
			}
		}
	}()
}

func detach(p *peer, key string) {
	if sender := p.sends[key]; sender != nil {
		if err := p.sub.RemoveTrack(sender); err != nil {
			slog.Warn("sfu: removing track", "err", err)
		}
		delete(p.sends, key)
	}
}

func (s *SFU) keyframeLocked(t *track) {
	if t.owner.pub == nil || time.Since(t.lastKF) < 300*time.Millisecond {
		return
	}
	t.lastKF = time.Now()
	_ = t.owner.pub.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(t.remote.SSRC())}})
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
