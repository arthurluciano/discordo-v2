package pion

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"discordo/server/internal/media"
)

// client is a participant: the same two peer connections a real client keeps.
type client struct {
	t      *testing.T
	sfu    *SFU
	userID string
	pub    *webrtc.PeerConnection
	sub    *webrtc.PeerConnection
	tracks chan *webrtc.TrackRemote
	names  map[string]string // what we publish: mid -> track name
}

func join(t *testing.T, sfu *SFU, userID string, grants media.Grants) *client {
	t.Helper()
	c := &client{t: t, sfu: sfu, userID: userID, tracks: make(chan *webrtc.TrackRemote, 8), names: map[string]string{}}
	var err error
	if c.pub, err = webrtc.NewPeerConnection(webrtc.Configuration{}); err != nil {
		t.Fatal(err)
	}
	if c.sub, err = webrtc.NewPeerConnection(webrtc.Configuration{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.pub.Close(); c.sub.Close() })
	c.sub.OnTrack(func(tr *webrtc.TrackRemote, _ *webrtc.RTPReceiver) { c.tracks <- tr })

	// Like the gateway, deliver asynchronously: the SFU calls send while holding its lock.
	send := func(typ string, data any) {
		sdp := data.(map[string]string)["sdp"]
		go func() {
			switch typ {
			case "RTC_ANSWER":
				if err := c.pub.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sdp}); err != nil {
					c.fail(err)
				}
			case "RTC_OFFER":
				if err := c.sub.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sdp}); err != nil {
					c.fail(err)
					return
				}
				answer, err := c.sub.CreateAnswer(nil)
				if err == nil {
					err = setLocal(c.sub, answer)
				}
				if err != nil {
					c.fail(err)
					return
				}
				c.signal("rtc_answer", map[string]any{"sdp": c.sub.LocalDescription().SDP})
			}
		}()
	}
	if _, err := sfu.Join(context.Background(), "room", userID, grants, send); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sfu.Leave("room", userID) })
	return c
}

func transceiverOf(pc *webrtc.PeerConnection, sender *webrtc.RTPSender) *webrtc.RTPTransceiver {
	for _, tr := range pc.GetTransceivers() {
		if tr.Sender() == sender {
			return tr
		}
	}
	panic("sender without a transceiver")
}

func (c *client) signal(typ string, data map[string]any) {
	raw, _ := json.Marshal(data)
	if err := c.sfu.Signal(context.Background(), "room", c.userID, typ, raw); err != nil {
		c.fail(fmt.Errorf("%s: %w", typ, err))
	}
}

// fail reports an error from a signaling goroutine, unless the test is already tearing down:
// by then connections are being closed under messages still in flight, which is expected.
func (c *client) fail(err error) {
	if c.t.Context().Err() == nil {
		c.t.Error(err)
	}
}

// publish starts sending a track with synthetic RTP until the test ends. Like a browser, the
// client cannot choose its track IDs, so it names the track by mid in the offer.
func (c *client) publish(name, mime string) {
	c.t.Helper()
	local, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: mime}, "random-"+name, c.userID)
	if err != nil {
		c.t.Fatal(err)
	}
	sender, err := c.pub.AddTrack(local)
	if err != nil {
		c.t.Fatal(err)
	}
	offer, err := c.pub.CreateOffer(nil)
	if err == nil {
		err = setLocal(c.pub, offer)
	}
	if err != nil {
		c.t.Fatal(err)
	}
	c.names[transceiverOf(c.pub, sender).Mid()] = name
	c.signal("rtc_offer", map[string]any{"sdp": c.pub.LocalDescription().SDP, "tracks": c.names})

	ctx := c.t.Context()
	go func() {
		for seq := uint16(0); ctx.Err() == nil; seq++ {
			_ = local.WriteRTP(&rtp.Packet{
				Header:  rtp.Header{Version: 2, SequenceNumber: seq, Timestamp: uint32(seq) * 960},
				Payload: []byte{0xDE, 0xAD, 0xBE, 0xEF},
			})
			time.Sleep(20 * time.Millisecond)
		}
	}()
}

// expect waits for the next incoming track and checks who and what it is, and that media actually flows.
func (c *client) expect(fromUser, name string) {
	c.t.Helper()
	select {
	case tr := <-c.tracks:
		if tr.StreamID() != fromUser || tr.ID() != name {
			c.t.Fatalf("%s got track %s/%s, want %s/%s", c.userID, tr.StreamID(), tr.ID(), fromUser, name)
		}
		tr.SetReadDeadline(time.Now().Add(10 * time.Second))
		if _, _, err := tr.ReadRTP(); err != nil {
			c.t.Fatalf("%s reading %s/%s: %v", c.userID, fromUser, name, err)
		}
	case <-time.After(15 * time.Second):
		c.t.Fatalf("%s never received %s/%s", c.userID, fromUser, name)
	}
}

func (c *client) expectNothing() {
	c.t.Helper()
	select {
	case tr := <-c.tracks:
		c.t.Fatalf("%s unexpectedly received %s/%s", c.userID, tr.StreamID(), tr.ID())
	case <-time.After(time.Second):
	}
}

func TestForwarding(t *testing.T) {
	sfu, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sfu.Close() })
	published := make(chan []string, 16)
	sfu.OnTracks(func(_, userID string, tracks []string) {
		if userID == "alice" {
			published <- tracks
		}
	})

	all := media.Grants{Speak: true, Stream: true}
	alice := join(t, sfu, "alice", all)
	bob := join(t, sfu, "bob", all)

	// Audio reaches everyone, including people who join later.
	alice.publish(media.Mic, webrtc.MimeTypeOpus)
	bob.expect("alice", media.Mic)
	carol := join(t, sfu, "carol", media.Grants{Speak: true})
	carol.expect("alice", media.Mic)
	alice.expectNothing() // nobody hears themselves

	// Video only goes to who asked for it.
	alice.publish(media.Screen, webrtc.MimeTypeH264)
	for got := range published {
		if len(got) == 2 { // [mic screen]
			break
		}
	}
	bob.expectNothing()
	bob.signal("rtc_subscribe", map[string]any{"user_id": "alice", "track": media.Screen, "on": true})
	bob.expect("alice", media.Screen)
	carol.expectNothing()

	// Without the Stream grant, video is published into the void.
	carol.publish(media.Camera, webrtc.MimeTypeH264)
	bob.signal("rtc_subscribe", map[string]any{"user_id": "carol", "track": media.Camera, "on": true})
	bob.expectNothing()

	// When the publisher leaves, their tracks are withdrawn.
	sfu.Leave("room", "alice")
	sfu.mu.Lock()
	defer sfu.mu.Unlock()
	if left := sfu.rooms["room"].published("alice"); len(left) != 0 {
		t.Fatalf("alice left but still publishes %v", left)
	}
	if len(sfu.rooms["room"].peers["bob"].sends) != 0 {
		t.Fatal("bob is still being sent alice's tracks")
	}
}

// publishSimulcast sends a video track in three layers, like a browser with sendEncodings. Each packet's payload is an
// H.264 NAL header (an SPS every 10th packet, so there are keyframes to switch on) followed by its layer's rid.
func (c *client) publishSimulcast(name string, rids ...string) {
	c.t.Helper()
	var layers []*webrtc.TrackLocalStaticRTP
	for _, rid := range rids {
		l, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "random-"+name, c.userID, webrtc.WithRTPStreamID(rid))
		if err != nil {
			c.t.Fatal(err)
		}
		layers = append(layers, l)
	}
	sender, err := c.pub.AddTrack(layers[0])
	if err != nil {
		c.t.Fatal(err)
	}
	for _, l := range layers[1:] {
		if err := sender.AddEncoding(l); err != nil {
			c.t.Fatal(err)
		}
	}
	offer, err := c.pub.CreateOffer(nil)
	if err == nil {
		err = setLocal(c.pub, offer)
	}
	if err != nil {
		c.t.Fatal(err)
	}
	mid := transceiverOf(c.pub, sender).Mid()
	c.names[mid] = name
	c.signal("rtc_offer", map[string]any{"sdp": c.pub.LocalDescription().SDP, "tracks": c.names})

	// pion does not stamp mid and rid on what it sends; the SFU needs them to tell the layers apart.
	var midExt, ridExt uint8
	for _, ext := range sender.GetParameters().HeaderExtensions {
		switch ext.URI {
		case "urn:ietf:params:rtp-hdrext:sdes:mid":
			midExt = uint8(ext.ID)
		case "urn:ietf:params:rtp-hdrext:sdes:rtp-stream-id":
			ridExt = uint8(ext.ID)
		}
	}
	ctx := c.t.Context()
	go func() {
		for seq := uint16(0); ctx.Err() == nil; seq++ {
			nal := byte(0x41) // non-IDR slice
			if seq%10 == 0 {
				nal = 0x67 // SPS: a keyframe starts here
			}
			for i, l := range layers {
				pkt := &rtp.Packet{
					// Each layer numbers its packets differently, as browsers do.
					Header:  rtp.Header{Version: 2, SequenceNumber: seq + uint16(i)*20000, Timestamp: uint32(seq) * 3000},
					Payload: []byte{nal, rids[i][0]},
				}
				_ = pkt.Header.SetExtension(midExt, []byte(mid))
				_ = pkt.Header.SetExtension(ridExt, []byte(l.RID()))
				_ = l.WriteRTP(pkt)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
}

func TestSimulcast(t *testing.T) {
	sfu, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sfu.Close() })
	all := media.Grants{Speak: true, Stream: true}
	alice := join(t, sfu, "alice", all)
	bob := join(t, sfu, "bob", all)

	alice.publishSimulcast(media.Screen, "h", "m", "l")
	bob.signal("rtc_subscribe", map[string]any{"user_id": "alice", "track": media.Screen, "on": true, "quality": "high"})
	var tr *webrtc.TrackRemote
	select {
	case tr = <-bob.tracks:
	case <-time.After(15 * time.Second):
		t.Fatal("bob never received alice's screen")
	}

	// read returns the layer of the next packet, checking it continues the stream bob has seen so far.
	var lastSeq uint16
	var lastTS uint32
	started := false
	read := func() byte {
		t.Helper()
		tr.SetReadDeadline(time.Now().Add(10 * time.Second))
		pkt, _, err := tr.ReadRTP()
		if err != nil {
			t.Fatal(err)
		}
		if started && (pkt.SequenceNumber != lastSeq+1 || int32(pkt.Timestamp-lastTS) < 0) {
			t.Fatalf("stream jumped from seq %d ts %d to seq %d ts %d", lastSeq, lastTS, pkt.SequenceNumber, pkt.Timestamp)
		}
		started, lastSeq, lastTS = true, pkt.SequenceNumber, pkt.Timestamp
		return pkt.Payload[1]
	}
	// switchTo reads until the stream is on the layer, then checks it stays there.
	switchTo := func(layer byte) {
		t.Helper()
		for i := 0; read() != layer; i++ {
			if i > 300 {
				t.Fatalf("never switched to layer %c", layer)
			}
		}
		for range 30 {
			if got := read(); got != layer {
				t.Fatalf("got a packet of layer %c while on %c", got, layer)
			}
		}
	}
	// The layers may reach the SFU in any order, so bob may start on a lower one before high arrives.
	switchTo('h')
	bob.signal("rtc_subscribe", map[string]any{"user_id": "alice", "track": media.Screen, "on": true, "quality": "low"})
	switchTo('l')
	bob.signal("rtc_subscribe", map[string]any{"user_id": "alice", "track": media.Screen, "on": true, "quality": "medium"})
	switchTo('m')
	bob.expectNothing() // changing quality is not a new track
}
