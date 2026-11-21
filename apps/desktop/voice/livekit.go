package voice

// Calls on a LiveKit server (VOICE_READY's connection kind "livekit"): the SDK carries the media, the gateway still
// carries the call's state (voice_state, the world). Publishing goes through publish, which does either.

import (
	"errors"

	lkproto "github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

// lkSources are LiveKit's sources for our track names, as the web and the server map them.
var lkSources = map[string]lkproto.TrackSource{
	"mic":            lkproto.TrackSource_MICROPHONE,
	TrackCamera:      lkproto.TrackSource_CAMERA,
	TrackScreen:      lkproto.TrackSource_SCREEN_SHARE,
	TrackScreenAudio: lkproto.TrackSource_SCREEN_SHARE_AUDIO,
}

func lkName(src lkproto.TrackSource) string {
	for name, s := range lkSources {
		if s == src {
			return name
		}
	}

	return ""
}

var lkQualities = map[string]lkproto.VideoQuality{
	"auto": lkproto.VideoQuality_HIGH, "high": lkproto.VideoQuality_HIGH,
	"medium": lkproto.VideoQuality_MEDIUM, "low": lkproto.VideoQuality_LOW,
}

// sampleTrack is what a published track is fed: a pion track (our SFU) or LiveKit's.
type sampleTrack interface{ WriteSample(media.Sample) error }

type lkSample struct{ *lksdk.LocalTrack }

func (t lkSample) WriteSample(s media.Sample) error { return t.LocalTrack.WriteSample(s, nil) }

// published is a track I send and how to take it off. On our SFU the caller negotiates after either.
type published struct {
	track  sampleTrack
	remove func() error
}

// publish sends a track named name in codec; onKeyframe (or nil) runs when a viewer asks for a keyframe.
func (s *Session) publish(name string, codec webrtc.RTPCodecCapability, onKeyframe func()) (published, error) {
	keyframe := func(p rtcp.Packet) {
		switch p.(type) {
		case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
			if onKeyframe != nil {
				onKeyframe()
			}
		}
	}

	s.mu.Lock()
	room := s.lk
	s.mu.Unlock()

	if room != nil {
		track, err := lksdk.NewLocalTrack(codec, lksdk.WithRTCPHandler(keyframe))
		if err != nil {
			return published{}, err
		}

		pub, err := room.LocalParticipant.PublishTrack(track, &lksdk.TrackPublicationOptions{Name: name, Source: lkSources[name]})
		if err != nil {
			return published{}, err
		}

		return published{track: lkSample{track}, remove: func() error { return room.LocalParticipant.UnpublishTrack(pub.SID()) }}, nil
	}

	track, err := webrtc.NewTrackLocalStaticSample(codec, name, "discordo")
	if err != nil {
		return published{}, err
	}

	s.pubMu.Lock()
	defer s.pubMu.Unlock()

	pub, err := s.ensurePub()
	if err != nil {
		return published{}, err
	}

	tr, err := pub.AddTransceiverFromTrack(track, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
	if err != nil {
		return published{}, err
	}

	s.mu.Lock()
	s.pubNames[tr] = name
	s.mu.Unlock()

	// Reading RTCP also runs the interceptors (NACK, reports).
	go func() {
		for {
			pkts, _, err := tr.Sender().ReadRTCP()
			if err != nil {
				return
			}

			for _, p := range pkts {
				keyframe(p)
			}
		}
	}()

	remove := func() error {
		s.pubMu.Lock()
		defer s.pubMu.Unlock()

		s.mu.Lock()
		delete(s.pubNames, tr)
		s.mu.Unlock()

		return pub.RemoveTrack(tr.Sender())
	}

	return published{track: track, remove: remove}, nil
}

// startLiveKit joins the LiveKit room VOICE_READY points at. Audio is always received; video only once watched.
func (s *Session) startLiveKit(url, token string) error {
	if url == "" || token == "" {
		return errors.New("o servidor não mandou o endereço da chamada")
	}

	cb := lksdk.NewRoomCallback()
	cb.OnTrackPublished = func(pub *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) { s.lkWant(pub, rp) }
	cb.OnTrackSubscribed = func(track *webrtc.TrackRemote, pub *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
		go s.lkReceive(track, pub, rp)
	}
	cb.OnDisconnected = func() {
		s.mu.Lock()
		closed := s.closed
		s.mu.Unlock()

		if !closed && s.ctx.Err() == nil {
			s.fail(msgMediaLost)
		}
	}

	room, err := lksdk.ConnectToRoomWithToken(url, token, cb, lksdk.WithAutoSubscribe(false))
	if err != nil {
		return err
	}

	s.mu.Lock()
	closed := s.closed
	if !closed {
		s.lk = room
	}
	s.mu.Unlock()

	if closed {
		room.Disconnect()
		return nil
	}

	for _, rp := range room.GetRemoteParticipants() {
		for _, p := range rp.TrackPublications() {
			if pub, ok := p.(*lksdk.RemoteTrackPublication); ok {
				s.lkWant(pub, rp)
			}
		}
	}

	return nil
}

// lkWant subscribes to a publication when it is audio or watched video.
func (s *Session) lkWant(pub *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
	name := lkName(pub.Source())
	if name == "" {
		return
	}

	s.mu.Lock()
	quality, watched := s.lkWatch[videoKey(rp.Identity(), name)]
	s.mu.Unlock()

	if pub.Kind() == lksdk.TrackKindAudio {
		_ = pub.SetSubscribed(true)
		return
	}

	if watched {
		_ = pub.SetSubscribed(true)
		if q, ok := lkQualities[quality]; ok {
			_ = pub.SetVideoQuality(q)
		}
	}
}

// lkSubscribe applies Watch and WatchQuality on LiveKit, now or when the track comes.
func (s *Session) lkSubscribe(room *lksdk.Room, userID, track string, on bool, quality string) {
	s.mu.Lock()
	if on {
		s.lkWatch[videoKey(userID, track)] = quality
	} else {
		delete(s.lkWatch, videoKey(userID, track))
	}
	s.mu.Unlock()

	rp := room.GetParticipantByIdentity(userID)
	if rp == nil {
		return
	}

	pub, _ := rp.GetTrackPublication(lkSources[track]).(*lksdk.RemoteTrackPublication)
	if pub == nil {
		return
	}

	if !on {
		_ = pub.SetSubscribed(false)
		return
	}

	s.lkWant(pub, rp)
}

// lkReceive plays or shows one subscribed track for as long as it lasts.
func (s *Session) lkReceive(track *webrtc.TrackRemote, pub *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
	if track.Kind() == webrtc.RTPCodecTypeAudio {
		s.receiveAudio(rp.Identity(), track)
		return
	}

	s.receiveVideo(rp.Identity(), lkName(pub.Source()), track)
}

// subLocked is the connection video comes in on (s.mu held): ours, or LiveKit's.
func (s *Session) subLocked() *webrtc.PeerConnection {
	if s.lk != nil {
		return s.lk.LocalParticipant.GetSubscriberPeerConnection()
	}

	return s.sub
}
