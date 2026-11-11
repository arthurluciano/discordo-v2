// Package voice tracks who is in which call and bridges the gateway to the media provider.
// A user is in at most one call, tied to the gateway session that joined it.
package voice

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"

	"discordo/server/internal/errs"
	"discordo/server/internal/event"
	"discordo/server/internal/media"
	"discordo/server/internal/permission"
)

// State is one user's presence in a call. An empty ChannelID means they left.
type State struct {
	UserID    string   `json:"user_id"`
	ChannelID string   `json:"channel_id"`
	GuildID   string   `json:"guild_id,omitempty"`
	Muted     bool     `json:"muted"`
	Deafened  bool     `json:"deafened"`
	Tracks    []string `json:"tracks"` // what they publish: mic, camera, screen, screen-audio
}

type Authorizer interface {
	// VoiceAccess fails unless the channel can host a call the user may see.
	// guildID is set for guild channels, recipients for DMs.
	VoiceAccess(ctx context.Context, userID, channelID string) (perms permission.Set, guildID string, recipients []string, err error)
}

type call struct {
	State
	sessionID  string
	recipients []string // DM calls
}

// Service implements gateway.Voice.
// shortcut: calls live in memory and permissions are checked on join only: losing Connect/Speak mid-call
// takes effect on the next join (being removed from the guild does end the call). Re-check on Sync events if that matters.
type Service struct {
	authz Authorizer
	media media.Provider
	pub   event.Publisher

	mu    sync.Mutex
	calls map[string]*call // by user ID
}

func NewService(authz Authorizer, provider media.Provider, pub event.Publisher) *Service {
	s := &Service{authz: authz, media: provider, pub: pub, calls: map[string]*call{}}
	provider.OnTracks(s.tracksChanged)
	return s
}

// Handle processes one gateway message from a client. Failures are reported to that client as VOICE_ERROR.
func (s *Service) Handle(ctx context.Context, userID, sessionID, op string, data json.RawMessage, send func(typ string, data any)) {
	var err error
	switch op {
	case "voice_join":
		var in struct {
			ChannelID string `json:"channel_id"`
		}
		if err = json.Unmarshal(data, &in); err == nil {
			err = s.join(ctx, userID, sessionID, in.ChannelID, send)
		}
	case "voice_leave":
		s.leave(userID, sessionID)
	case "voice_state":
		var in struct{ Muted, Deafened bool }
		if err = json.Unmarshal(data, &in); err == nil {
			err = s.setFlags(userID, sessionID, in.Muted, in.Deafened)
		}
	case "rtc_offer", "rtc_answer", "rtc_subscribe":
		s.mu.Lock()
		c := s.calls[userID]
		s.mu.Unlock()
		if c == nil || c.sessionID != sessionID {
			err = errs.Invalid("not in a call")
		} else {
			err = s.media.Signal(ctx, c.ChannelID, userID, op, data)
		}
	default:
		err = errs.Invalid("unknown op")
	}
	if err == nil {
		return
	}
	msg := "internal error"
	var syntax *json.SyntaxError
	switch {
	case errors.Is(err, errs.ErrInvalid), errors.Is(err, errs.ErrForbidden), errors.Is(err, errs.ErrNotFound):
		msg = err.Error()
	case errors.As(err, &syntax):
		msg = "malformed message"
	default:
		slog.Error("voice: handling message", "op", op, "err", err)
	}
	send("VOICE_ERROR", map[string]string{"op": op, "error": msg})
}

func (s *Service) join(ctx context.Context, userID, sessionID, channelID string, send func(string, any)) error {
	perms, guildID, recipients, err := s.authz.VoiceAccess(ctx, userID, channelID)
	if err != nil {
		return err
	}
	if !perms.Has(permission.Connect) {
		return errs.ErrForbidden
	}
	s.leave(userID, "") // one call per user: joining here hangs up anywhere else

	// VOICE_READY goes first so the client knows the transport before any media signaling reaches it.
	conn := media.Connection{Kind: "pion"}
	c := &call{sessionID: sessionID, recipients: recipients,
		State: State{UserID: userID, ChannelID: channelID, GuildID: guildID, Tracks: []string{}}}
	s.mu.Lock()
	s.calls[userID] = c
	s.mu.Unlock()
	send("VOICE_READY", map[string]any{"channel_id": channelID, "connection": conn})

	grants := media.Grants{Speak: perms.Has(permission.Speak), Stream: perms.Has(permission.Stream)}
	if _, err := s.media.Join(ctx, channelID, userID, grants, send); err != nil {
		s.mu.Lock()
		delete(s.calls, userID)
		s.mu.Unlock()
		return err
	}
	s.publish(c, c.State)
	return nil
}

// leave ends the user's call. With a sessionID it only does so if that session owns the call.
func (s *Service) leave(userID, sessionID string) {
	s.mu.Lock()
	c := s.calls[userID]
	if c == nil || (sessionID != "" && c.sessionID != sessionID) {
		s.mu.Unlock()
		return
	}
	delete(s.calls, userID)
	s.mu.Unlock()

	s.media.Leave(c.ChannelID, userID)
	s.publish(c, State{UserID: userID, GuildID: c.GuildID, Tracks: []string{}})
}

// Disconnected is called when a gateway session ends.
func (s *Service) Disconnected(userID, sessionID string) { s.leave(userID, sessionID) }

// DropFromGuild ends the user's call if it is in that guild (they left it or were removed).
func (s *Service) DropFromGuild(guildID, userID string) {
	s.mu.Lock()
	c := s.calls[userID]
	s.mu.Unlock()
	if c != nil && c.GuildID == guildID {
		s.leave(userID, "")
	}
}

func (s *Service) setFlags(userID, sessionID string, muted, deafened bool) error {
	s.mu.Lock()
	c := s.calls[userID]
	if c == nil || c.sessionID != sessionID {
		s.mu.Unlock()
		return errs.Invalid("not in a call")
	}
	c.Muted, c.Deafened = muted || deafened, deafened // deafened implies muted
	st := c.State
	s.mu.Unlock()
	s.publish(c, st)
	return nil
}

func (s *Service) tracksChanged(channelID, userID string, tracks []string) {
	s.mu.Lock()
	c := s.calls[userID]
	if c == nil || c.ChannelID != channelID {
		s.mu.Unlock()
		return
	}
	c.Tracks = tracks
	st := c.State
	s.mu.Unlock()
	s.publish(c, st)
}

// publish tells everyone who can see the call's channel. st is passed by value: c may change concurrently.
func (s *Service) publish(c *call, st State) {
	ev := event.Event{Kind: event.ToUsers, UserIDs: c.recipients, Type: "VOICE_STATE_UPDATE", Data: st}
	if c.GuildID != "" {
		ev = event.Event{Kind: event.ToChannel, GuildID: c.GuildID, ChannelID: c.ChannelID, Type: "VOICE_STATE_UPDATE", Data: st}
	}
	s.pub.Publish(ev)
}

// States returns every current call state; the caller filters by what its user may see.
func (s *Service) States() []State {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]State, 0, len(s.calls))
	for _, c := range s.calls {
		out = append(out, c.State)
	}
	return out
}
