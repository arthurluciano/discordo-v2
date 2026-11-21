// Package voice tracks who is in which call and bridges the gateway to the media provider.
// A user is in at most one call, tied to the gateway session that joined it.
package voice

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"discordo/server/internal/errs"
	"discordo/server/internal/event"
	"discordo/server/internal/media"
	"discordo/server/internal/permission"
	"discordo/server/internal/world"
)

// State is one user's presence in a call. An empty ChannelID means they left.
type State struct {
	UserID    string   `json:"user_id"`
	ChannelID string   `json:"channel_id"`
	GuildID   string   `json:"guild_id,omitempty"`
	Muted     bool     `json:"muted"`
	Deafened  bool     `json:"deafened"`
	Tracks    []string `json:"tracks"` // what they publish: mic, camera, screen, screen-audio

	// In a world: where they stand, and the room they are in ("" in the plaza).
	X    float32 `json:"x,omitempty"`
	Y    float32 `json:"y,omitempty"`
	Zone string  `json:"zone,omitempty"`
	Skin string  `json:"skin,omitempty"` // a world.Skins ID worn instead of the character made up from user_id
}

// Access is what a user may do in a channel that can host a call.
type Access struct {
	Perms      permission.Set
	GuildID    string   // guild channels
	Recipients []string // DMs
	World      bool     // the guild's world: the call has positions
}

type Authorizer interface {
	// VoiceAccess fails unless the channel can host a call the user may see.
	VoiceAccess(ctx context.Context, userID, channelID string) (Access, error)
}

type call struct {
	State
	sessionID  string
	recipients []string // DM calls
	world      bool
	moved      time.Time // the last world_move taken
	reacted    time.Time // the last world_react taken
}

// moveEvery is the least time between two moves of one user: clients send 10 a second, more is dropped.
const moveEvery = 40 * time.Millisecond

// reactEvery is the least time between two reactions of one user: a held key must not flood the room.
const reactEvery = 250 * time.Millisecond

// Reactions are what world_react may send: the chat's reactions, so every client already draws them.
var Reactions = []string{"👍", "❤️", "😂", "😮", "😢", "🎉", "🔥", "👀"}

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
	case "world_move":
		var in struct{ X, Y float32 }
		if err = json.Unmarshal(data, &in); err == nil {
			err = s.move(userID, sessionID, in.X, in.Y)
		}
	case "world_skin":
		var in struct{ Skin string }
		if err = json.Unmarshal(data, &in); err == nil {
			err = s.setSkin(userID, sessionID, in.Skin)
		}
	case "world_react":
		var in struct{ Emoji string }
		if err = json.Unmarshal(data, &in); err == nil {
			err = s.react(userID, sessionID, in.Emoji)
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
	access, err := s.authz.VoiceAccess(ctx, userID, channelID)
	if err != nil {
		return err
	}
	perms := access.Perms
	if !perms.Has(permission.Connect) {
		return errs.ErrForbidden
	}
	s.leave(userID, "") // one call per user: joining here hangs up anywhere else

	c := &call{sessionID: sessionID, recipients: access.Recipients, world: access.World,
		State: State{UserID: userID, ChannelID: channelID, GuildID: access.GuildID, Tracks: []string{}}}
	if c.world {
		c.X, c.Y = world.Default.Spawn.X, world.Default.Spawn.Y
	}
	s.mu.Lock()
	s.calls[userID] = c
	s.mu.Unlock()

	// VOICE_READY carries what Join returns, yet must reach the client before any signaling Join itself sends.
	var held sync.Mutex
	var early []func()
	ready := false
	relay := func(typ string, data any) {
		held.Lock()
		defer held.Unlock()
		if ready {
			send(typ, data)
		} else {
			early = append(early, func() { send(typ, data) })
		}
	}
	grants := media.Grants{Speak: perms.Has(permission.Speak), Stream: perms.Has(permission.Stream)}
	conn, err := s.media.Join(ctx, channelID, userID, grants, relay)
	if err != nil {
		s.mu.Lock()
		delete(s.calls, userID)
		s.mu.Unlock()
		return err
	}
	held.Lock()
	send("VOICE_READY", map[string]any{"channel_id": channelID, "connection": conn})
	for _, f := range early {
		f()
	}
	ready = true
	held.Unlock()
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

// setSkin dresses the user in a skin, or back in their own character with "".
func (s *Service) setSkin(userID, sessionID, skin string) error {
	if skin != "" && !world.HasSkin(skin) {
		return errs.Invalid("unknown skin")
	}

	s.mu.Lock()
	c := s.calls[userID]
	if c == nil || c.sessionID != sessionID || !c.world {
		s.mu.Unlock()
		return errs.Invalid("not in a world")
	}
	c.Skin = skin
	st := c.State
	s.mu.Unlock()
	s.publish(c, st)
	return nil
}

// react floats an emoji over the user's head for the others in their room (in the plaza, for those in the plaza).
// Like moves, those faster than reactEvery are dropped without an error.
func (s *Service) react(userID, sessionID, emoji string) error {
	if !slices.Contains(Reactions, emoji) {
		return errs.Invalid("unknown reaction")
	}

	s.mu.Lock()
	c := s.calls[userID]
	if c == nil || c.sessionID != sessionID || !c.world {
		s.mu.Unlock()
		return errs.Invalid("not in a world")
	}

	now := time.Now()
	if now.Sub(c.reacted) < reactEvery {
		s.mu.Unlock()
		return nil
	}
	c.reacted = now

	var peers []string
	for id, other := range s.calls {
		if id != userID && other.ChannelID == c.ChannelID && other.Zone == c.Zone {
			peers = append(peers, id)
		}
	}
	channelID := c.ChannelID
	s.mu.Unlock()

	s.pub.Publish(event.Event{Kind: event.ToUsers, UserIDs: peers, Type: "WORLD_REACT",
		Data: map[string]any{"channel_id": channelID, "user_id": userID, "emoji": emoji}})
	return nil
}

// move puts the user somewhere else in the world. The others in the call get the light WORLD_MOVE;
// walking into or out of a room is a VOICE_STATE_UPDATE, which the whole guild sees ("Jogando Valorant").
// shortcut: walls are the client's business and audio reaches everyone in the call, with the silence between
// rooms applied by each client; subscribe audio per room in the SFU if a modified client listening in matters.
func (s *Service) move(userID, sessionID string, x, y float32) error {
	if !world.Default.Contains(x, y) {
		return errs.Invalid("position outside the map")
	}

	s.mu.Lock()
	c := s.calls[userID]
	if c == nil || c.sessionID != sessionID || !c.world {
		s.mu.Unlock()
		return errs.Invalid("not in a world")
	}

	now := time.Now()
	if now.Sub(c.moved) < moveEvery {
		s.mu.Unlock()
		return nil
	}

	zone := world.Default.ZoneAt(x, y)
	changed := zone != c.Zone
	c.moved, c.X, c.Y, c.Zone = now, x, y, zone
	st := c.State

	var peers []string
	for id, other := range s.calls {
		if id != userID && other.ChannelID == c.ChannelID {
			peers = append(peers, id)
		}
	}
	s.mu.Unlock()

	if changed {
		s.publish(c, st)
		return nil
	}

	s.pub.Publish(event.Event{Kind: event.ToUsers, UserIDs: peers, Type: "WORLD_MOVE",
		Data: map[string]any{"channel_id": st.ChannelID, "user_id": userID, "x": x, "y": y}})
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
