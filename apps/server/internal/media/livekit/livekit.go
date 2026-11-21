// Package livekit hands calls to a LiveKit server: we mint access tokens, LiveKit carries the media and
// tells us through webhooks what each participant publishes.
package livekit

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/livekit/protocol/auth"
	lkproto "github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/webhook"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/twitchtv/twirp"

	"discordo/server/internal/errs"
	"discordo/server/internal/media"
)

type Config struct {
	URL       string // what clients connect to (wss://...)
	APIURL    string // where we reach LiveKit (http://livekit:7880); also proxied at /rtc when set. "": URL
	APIKey    string
	APISecret string
}

// tokenTTL only has to cover connecting: once in, LiveKit refreshes the token over the connection.
const tokenTTL = 2 * time.Minute

var sources = map[lkproto.TrackSource]string{
	lkproto.TrackSource_MICROPHONE:         media.Mic,
	lkproto.TrackSource_CAMERA:             media.Camera,
	lkproto.TrackSource_SCREEN_SHARE:       media.Screen,
	lkproto.TrackSource_SCREEN_SHARE_AUDIO: media.ScreenAudio,
}

// Provider implements media.Provider and, for LiveKit's webhooks, http.Handler.
type Provider struct {
	cfg      Config
	rooms    *lksdk.RoomServiceClient
	keys     auth.KeyProvider
	proxy    http.Handler // to APIURL's signalling, or nil
	onTracks func(roomID, userID string, tracks []string)
}

func New(cfg Config) (*Provider, error) {
	api, proxy := cfg.URL, http.Handler(nil)
	if cfg.APIURL != "" {
		target, err := url.Parse(cfg.APIURL)
		if err != nil {
			return nil, err
		}

		api, proxy = cfg.APIURL, httputil.NewSingleHostReverseProxy(target) // it carries WebSocket upgrades
	}

	return &Provider{
		cfg:      cfg,
		rooms:    lksdk.NewRoomServiceClient(api, cfg.APIKey, cfg.APISecret),
		proxy:    proxy,
		keys:     auth.NewSimpleKeyProvider(cfg.APIKey, cfg.APISecret),
		onTracks: func(string, string, []string) {},
	}, nil
}

// SignalProxy passes clients' /rtc requests on to LiveKit, when it is reached at APIURL.
func (p *Provider) SignalProxy() http.Handler { return p.proxy }

func (p *Provider) OnTracks(fn func(roomID, userID string, tracks []string)) { p.onTracks = fn }

// Join only mints a token: the client connects to LiveKit itself, and a second connection with the same identity
// makes LiveKit drop the first.
func (p *Provider) Join(_ context.Context, roomID, userID string, grants media.Grants, _ media.Sender) (media.Connection, error) {
	token, err := p.token(roomID, userID, grants)
	return media.Connection{Kind: "livekit", URL: p.cfg.URL, Token: token}, err
}

func (p *Provider) token(roomID, userID string, grants media.Grants) (string, error) {
	var allowed []lkproto.TrackSource
	if grants.Speak {
		allowed = append(allowed, lkproto.TrackSource_MICROPHONE)
	}
	if grants.Stream {
		allowed = append(allowed, lkproto.TrackSource_CAMERA, lkproto.TrackSource_SCREEN_SHARE, lkproto.TrackSource_SCREEN_SHARE_AUDIO)
	}
	grant := &auth.VideoGrant{RoomJoin: true, Room: roomID}
	grant.SetCanSubscribe(true)
	// An empty source list means "any source" to LiveKit, so no grant must turn publishing off explicitly.
	grant.SetCanPublish(len(allowed) > 0)
	grant.SetCanPublishSources(allowed)
	return auth.NewAccessToken(p.cfg.APIKey, p.cfg.APISecret).
		SetIdentity(userID).SetValidFor(tokenTTL).SetVideoGrant(grant).ToJWT()
}

func (p *Provider) Signal(context.Context, string, string, string, json.RawMessage) error {
	return errs.Invalid("this server's calls do not signal over the gateway")
}

// Leave is synchronous so a rejoin right after it is not kicked by a late removal.
func (p *Provider) Leave(roomID, userID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := p.rooms.RemoveParticipant(ctx, &lkproto.RoomParticipantIdentity{Room: roomID, Identity: userID})
	var te twirp.Error
	if err != nil && !(errors.As(err, &te) && te.Code() == twirp.NotFound) {
		slog.Warn("livekit: removing participant", "room", roomID, "user", userID, "err", err)
	}
}

// ServeHTTP receives LiveKit's webhooks, signed with our API key.
func (p *Provider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ev, err := webhook.ReceiveWebhookEvent(r, p.keys)
	if err != nil {
		http.Error(w, "invalid webhook", http.StatusUnauthorized)
		return
	}
	if roomID, userID, tracks, ok := trackUpdate(ev); ok {
		p.onTracks(roomID, userID, tracks)
	}
}

// trackUpdate says what the event's participant now publishes, if the event changes that.
// The event's track is applied on top of the participant's list, which may not reflect it yet.
func trackUpdate(ev *lkproto.WebhookEvent) (roomID, userID string, tracks []string, ok bool) {
	if ev.Room == nil || ev.Participant == nil {
		return "", "", nil, false
	}
	roomID, userID, tracks = ev.Room.Name, ev.Participant.Identity, []string{}
	switch ev.Event {
	case webhook.EventParticipantLeft:
		return roomID, userID, tracks, true
	case webhook.EventTrackPublished, webhook.EventTrackUnpublished:
	default:
		return "", "", nil, false
	}
	changed := ""
	if ev.Track != nil {
		changed = ev.Track.Sid
	}
	for _, t := range ev.Participant.Tracks {
		if name, known := sources[t.Source]; known && t.Sid != changed {
			tracks = append(tracks, name)
		}
	}
	if name, known := sources[ev.Track.GetSource()]; known && ev.Event == webhook.EventTrackPublished {
		tracks = append(tracks, name)
	}
	return roomID, userID, tracks, true
}
