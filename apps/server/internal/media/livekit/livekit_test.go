package livekit

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/livekit/protocol/auth"
	lkproto "github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/utils/protojson"

	"discordo/server/internal/media"
)

const key, secret = "testkey", "testsecret-that-is-long-enough-for-hs256"

func TestTokenGrants(t *testing.T) {
	p, _ := New(Config{URL: "ws://livekit.test", APIKey: key, APISecret: secret})
	all := []lkproto.TrackSource{lkproto.TrackSource_MICROPHONE, lkproto.TrackSource_CAMERA, lkproto.TrackSource_SCREEN_SHARE, lkproto.TrackSource_SCREEN_SHARE_AUDIO}
	for _, tc := range []struct {
		grants  media.Grants
		allowed []lkproto.TrackSource
	}{
		{media.Grants{}, nil},
		{media.Grants{Speak: true}, all[:1]},
		{media.Grants{Stream: true}, all[1:]},
		{media.Grants{Speak: true, Stream: true}, all},
	} {
		conn, err := p.Join(t.Context(), "room1", "user1", tc.grants, nil)
		if err != nil {
			t.Fatal(err)
		}
		if conn.Kind != "livekit" || conn.URL != "ws://livekit.test" {
			t.Fatalf("connection %+v", conn)
		}
		v, err := auth.ParseAPIToken(conn.Token)
		if err != nil {
			t.Fatal(err)
		}
		claims, grants, err := v.Verify(secret)
		if err != nil {
			t.Fatal(err)
		}
		if grants.Identity != "user1" || claims.ExpiresAt.After(time.Now().Add(tokenTTL+time.Minute)) {
			t.Fatalf("identity %q, expiry %v", grants.Identity, claims.ExpiresAt)
		}
		g := grants.Video
		if !g.RoomJoin || g.Room != "room1" || !g.GetCanSubscribe() {
			t.Fatalf("grant %+v", g)
		}
		for _, s := range all {
			if want := slices.Contains(tc.allowed, s); g.GetCanPublishSource(s) != want {
				t.Errorf("%+v: publish %v = %v, want %v", tc.grants, s, !want, want)
			}
		}
	}
}

func TestSourceNames(t *testing.T) {
	names := map[string]bool{}
	for _, n := range sources {
		names[n] = true
	}
	for _, n := range []string{media.Mic, media.Camera, media.Screen, media.ScreenAudio} {
		if !names[n] {
			t.Errorf("no LiveKit source for %q", n)
		}
	}
	if len(sources) != 4 {
		t.Errorf("%d sources, want 4", len(sources))
	}
}

func TestWebhook(t *testing.T) {
	p, _ := New(Config{URL: "ws://livekit.test", APIKey: key, APISecret: secret})
	type update struct {
		room, user string
		tracks     []string
	}
	var got []update
	p.OnTracks(func(room, user string, tracks []string) { got = append(got, update{room, user, tracks}) })

	mic := &lkproto.TrackInfo{Sid: "TR_mic", Source: lkproto.TrackSource_MICROPHONE}
	screen := &lkproto.TrackInfo{Sid: "TR_screen", Source: lkproto.TrackSource_SCREEN_SHARE}
	room := &lkproto.Room{Name: "room1"}
	participant := func(tracks ...*lkproto.TrackInfo) *lkproto.ParticipantInfo {
		return &lkproto.ParticipantInfo{Identity: "user1", Tracks: tracks}
	}
	for _, tc := range []struct {
		ev   *lkproto.WebhookEvent
		want []string // nil: no update
	}{
		// The participant's list may or may not include the track the event is about yet.
		{&lkproto.WebhookEvent{Event: "track_published", Room: room, Participant: participant(), Track: mic}, []string{"mic"}},
		{&lkproto.WebhookEvent{Event: "track_published", Room: room, Participant: participant(mic, screen), Track: screen}, []string{"mic", "screen"}},
		{&lkproto.WebhookEvent{Event: "track_unpublished", Room: room, Participant: participant(mic, screen), Track: screen}, []string{"mic"}},
		{&lkproto.WebhookEvent{Event: "participant_left", Room: room, Participant: participant(mic)}, []string{}},
		{&lkproto.WebhookEvent{Event: "participant_joined", Room: room, Participant: participant()}, nil},
	} {
		got = nil
		if code := deliver(t, p, tc.ev, secret); code != http.StatusOK {
			t.Fatalf("%s: status %d", tc.ev.Event, code)
		}
		var want []update
		if tc.want != nil {
			want = []update{{"room1", "user1", tc.want}}
		}
		if !slices.EqualFunc(got, want, func(a, b update) bool {
			return a.room == b.room && a.user == b.user && slices.Equal(a.tracks, b.tracks)
		}) {
			t.Errorf("%s: got %+v, want %+v", tc.ev.Event, got, want)
		}
	}

	got = nil
	ev := &lkproto.WebhookEvent{Event: "participant_left", Room: room, Participant: participant()}
	if code := deliver(t, p, ev, "some-other-secret-that-is-long-enough"); code != http.StatusUnauthorized || got != nil {
		t.Fatalf("forged webhook: status %d, updates %+v", code, got)
	}
}

// deliver posts ev the way LiveKit does: a JWT carrying the body's SHA-256, signed with the API secret.
func deliver(t *testing.T, h http.Handler, ev *lkproto.WebhookEvent, secret string) int {
	t.Helper()
	body, err := protojson.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	token, err := auth.NewAccessToken(key, secret).SetValidFor(time.Minute).SetSha256(base64.StdEncoding.EncodeToString(sum[:])).ToJWT()
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/livekit/webhook", bytes.NewReader(body))
	r.Header.Set("Authorization", token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

func TestSignalProxy(t *testing.T) {
	lk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(r.URL.Path)) }))
	defer lk.Close()

	p, err := New(Config{URL: "wss://discordo.test", APIURL: lk.URL, APIKey: "k", APISecret: "s"})
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	p.SignalProxy().ServeHTTP(rec, httptest.NewRequest("GET", "/rtc/validate", nil))
	if rec.Body.String() != "/rtc/validate" {
		t.Fatalf("proxied to %q", rec.Body.String())
	}

	if p, _ := New(Config{URL: "wss://discordo.test"}); p.SignalProxy() != nil {
		t.Fatal("a proxy without LIVEKIT_API_URL")
	}
}
