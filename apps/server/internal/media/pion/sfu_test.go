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
}

func join(t *testing.T, sfu *SFU, userID string, grants media.Grants) *client {
	t.Helper()
	c := &client{t: t, sfu: sfu, userID: userID, tracks: make(chan *webrtc.TrackRemote, 8)}
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

// publish starts sending a track with synthetic RTP until the test ends.
func (c *client) publish(name, mime string) {
	c.t.Helper()
	local, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: mime}, name, c.userID)
	if err != nil {
		c.t.Fatal(err)
	}
	if _, err := c.pub.AddTrack(local); err != nil {
		c.t.Fatal(err)
	}
	offer, err := c.pub.CreateOffer(nil)
	if err == nil {
		err = setLocal(c.pub, offer)
	}
	if err != nil {
		c.t.Fatal(err)
	}
	c.signal("rtc_offer", map[string]any{"sdp": c.pub.LocalDescription().SDP})

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
