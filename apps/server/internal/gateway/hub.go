// Package gateway is the realtime side: one WebSocket per client, fed by domain events.
//
// Protocol: the client's first message is {"token": "..."} (or {} to use the session cookie); every server message is
// {"t": TYPE, "d": DATA}, starting with READY. After that the client may send {"op": OP, "d": DATA}
// for calls (see the voice package). Delivery is at-least-once around (re)connects,
// so clients must apply events idempotently (by ID).
package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"discordo/server/internal/auth"
	"discordo/server/internal/event"
	"discordo/server/internal/id"
	"discordo/server/internal/user"
)

type Authenticator interface {
	Authenticate(ctx context.Context, token string) (user.User, error)
}

type Guilds interface {
	UserGuildIDs(ctx context.Context, userID string) ([]string, error)
	Snapshot(ctx context.Context, userID, guildID string) (any, error)
	Viewers(ctx context.Context, channelID string) (map[string]bool, error)
}

// Social is the per-user data outside guilds.
type Social interface {
	// Private returns extra READY fields that only this user may see (DMs, relationships).
	Private(ctx context.Context, userID string) (map[string]any, error)
	FriendIDs(ctx context.Context, userID string) ([]string, error)
}

// Voice handles the client's call and media-signaling messages.
type Voice interface {
	Handle(ctx context.Context, userID, sessionID, op string, data json.RawMessage, send func(typ string, data any))
	Disconnected(userID, sessionID string)
}

type session struct {
	id     string
	userID string
	guilds map[string]struct{} // guarded by Hub.mu
	send   chan []byte
	kick   context.CancelFunc
}

// push never blocks the dispatcher: a client that cannot keep up is disconnected and resyncs on reconnect.
func (s *session) push(b []byte) {
	select {
	case s.send <- b:
	default:
		s.kick()
	}
}

// Hub implements event.Publisher and guild.Presence.
// shortcut: in-process fan-out, single server instance; put NATS/Redis behind Publish to scale horizontally.
type Hub struct {
	// Set before serving; fields because these services in turn depend on the hub.
	Guilds Guilds
	Social Social
	Voice  Voice

	auth  Authenticator
	queue chan event.Event

	mu      sync.RWMutex
	byUser  map[string]map[*session]struct{}
	byGuild map[string]map[*session]struct{}
}

func NewHub(auth Authenticator) *Hub {
	return &Hub{
		auth:    auth,
		queue:   make(chan event.Event, 1024),
		byUser:  map[string]map[*session]struct{}{},
		byGuild: map[string]map[*session]struct{}{},
	}
}

// Publish queues an event; a single dispatcher keeps them in order. Blocks when the queue is full (backpressure).
func (h *Hub) Publish(ev event.Event) { h.queue <- ev }

func (h *Hub) Online(userID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.byUser[userID]) > 0
}

// Run dispatches events until ctx is done.
func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-h.queue:
			h.dispatch(ctx, ev)
		}
	}
}

func (h *Hub) dispatch(ctx context.Context, ev event.Event) {
	switch ev.Kind {
	case event.ToGuild:
		send(h.sessions(h.byGuild, ev.GuildID), ev.Type, ev.Data)
	case event.ToChannel:
		viewers, err := h.Guilds.Viewers(ctx, ev.ChannelID)
		if err != nil {
			slog.Warn("gateway: resolving channel viewers", "channel", ev.ChannelID, "err", err)
			return
		}
		var ss []*session
		for _, s := range h.sessions(h.byGuild, ev.GuildID) {
			if viewers[s.userID] {
				ss = append(ss, s)
			}
		}
		send(ss, ev.Type, ev.Data)
	case event.ToUsers:
		send(h.sessions(h.byUser, ev.UserIDs...), ev.Type, ev.Data)
	case event.Sync:
		h.sync(ctx, ev.GuildID, h.sessions(h.byGuild, ev.GuildID))
	case event.Joined:
		ss := h.sessions(h.byUser, ev.UserIDs...)
		h.mu.Lock()
		for _, s := range ss {
			s.guilds[ev.GuildID] = struct{}{}
			put(h.byGuild, ev.GuildID, s)
		}
		h.mu.Unlock()
		h.sync(ctx, ev.GuildID, ss)
	case event.Left:
		ss := h.sessions(h.byUser, ev.UserIDs...)
		h.mu.Lock()
		for _, s := range ss {
			delete(s.guilds, ev.GuildID)
			drop(h.byGuild, ev.GuildID, s)
		}
		h.mu.Unlock()
		send(ss, "GUILD_DELETE", map[string]string{"id": ev.GuildID})
	}
}

// sync sends each session its own view of the guild.
func (h *Hub) sync(ctx context.Context, guildID string, ss []*session) {
	perUser := map[string][]byte{}
	for _, s := range ss {
		b, ok := perUser[s.userID]
		if !ok {
			snap, err := h.Guilds.Snapshot(ctx, s.userID, guildID)
			if err != nil {
				slog.Warn("gateway: snapshot", "guild", guildID, "err", err)
				continue
			}
			b = encode("GUILD_SYNC", snap)
			perUser[s.userID] = b
		}
		s.push(b)
	}
}

func (h *Hub) sessions(index map[string]map[*session]struct{}, keys ...string) []*session {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var ss []*session
	for _, k := range keys {
		for s := range index[k] {
			ss = append(ss, s)
		}
	}
	return ss
}

func send(ss []*session, typ string, data any) {
	if len(ss) == 0 {
		return
	}
	b := encode(typ, data)
	for _, s := range ss {
		s.push(b)
	}
}

func encode(typ string, data any) []byte {
	b, err := json.Marshal(struct {
		T string `json:"t"`
		D any    `json:"d"`
	}{typ, data})
	if err != nil {
		slog.Error("gateway: encoding event", "type", typ, "err", err)
	}
	return b
}

func put(index map[string]map[*session]struct{}, key string, s *session) {
	if index[key] == nil {
		index[key] = map[*session]struct{}{}
	}
	index[key][s] = struct{}{}
}

func drop(index map[string]map[*session]struct{}, key string, s *session) {
	delete(index[key], s)
	if len(index[key]) == 0 {
		delete(index, key)
	}
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	var hello struct {
		Token string `json:"token"`
	}
	helloCtx, helloCancel := context.WithTimeout(ctx, 10*time.Second)
	err = wsjson.Read(helloCtx, c, &hello)
	helloCancel()
	if err != nil {
		return
	}
	if hello.Token == "" { // the web app: its session cookie came with the upgrade request
		if c, err := r.Cookie(auth.SessionCookie); err == nil {
			hello.Token = c.Value
		}
	}
	u, err := h.auth.Authenticate(ctx, hello.Token)
	if err != nil {
		c.Close(websocket.StatusPolicyViolation, "unauthorized")
		return
	}
	guildIDs, err := h.Guilds.UserGuildIDs(ctx, u.ID)
	if err != nil {
		c.Close(websocket.StatusInternalError, "")
		return
	}
	ready, err := h.Social.Private(ctx, u.ID)
	if err != nil {
		c.Close(websocket.StatusInternalError, "")
		return
	}

	// Subscribe before building READY so nothing published in between is lost.
	s := &session{id: id.New(), userID: u.ID, guilds: map[string]struct{}{}, send: make(chan []byte, 256), kick: cancel}
	h.add(s, guildIDs)
	defer h.remove(s)
	defer h.Voice.Disconnected(u.ID, s.id)

	snaps := make([]any, 0, len(guildIDs))
	for _, g := range guildIDs {
		if snap, err := h.Guilds.Snapshot(ctx, u.ID, g); err == nil { // an error here means they just left it
			snaps = append(snaps, snap)
		}
	}
	// Client messages are handled one at a time, in order. Reading also answers pings and notices the close.
	c.SetReadLimit(256 << 10) // an SDP with many tracks is far larger than the default limit
	go func() {
		defer cancel()
		for {
			var msg struct {
				Op string          `json:"op"`
				D  json.RawMessage `json:"d"`
			}
			if wsjson.Read(ctx, c, &msg) != nil {
				return
			}
			h.Voice.Handle(ctx, u.ID, s.id, msg.Op, msg.D, func(typ string, data any) { s.push(encode(typ, data)) })
		}
	}()
	write := func(b []byte) error {
		wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
		defer wcancel()
		return c.Write(wctx, websocket.MessageText, b)
	}
	ready["user"], ready["guilds"] = u, snaps
	if write(encode("READY", ready)) != nil {
		return
	}
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case b := <-s.send:
			if write(b) != nil {
				return
			}
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.Ping(pctx)
			pcancel()
			if err != nil {
				return
			}
		}
	}
}

func (h *Hub) add(s *session, guildIDs []string) {
	h.mu.Lock()
	first := len(h.byUser[s.userID]) == 0
	put(h.byUser, s.userID, s)
	for _, g := range guildIDs {
		s.guilds[g] = struct{}{}
		put(h.byGuild, g, s)
	}
	h.mu.Unlock()
	if first {
		h.presence(s.userID, guildIDs, true)
	}
}

func (h *Hub) remove(s *session) {
	h.mu.Lock()
	drop(h.byUser, s.userID, s)
	last := len(h.byUser[s.userID]) == 0
	guildIDs := make([]string, 0, len(s.guilds))
	for g := range s.guilds {
		guildIDs = append(guildIDs, g)
		drop(h.byGuild, g, s)
	}
	h.mu.Unlock()
	if last {
		h.presence(s.userID, guildIDs, false)
	}
}

// presence tells everyone who shares a guild with the user, and their friends. Recipients may get it more than once.
func (h *Hub) presence(userID string, guildIDs []string, online bool) {
	data := map[string]any{"user_id": userID, "online": online}
	for _, g := range guildIDs {
		h.Publish(event.Event{Kind: event.ToGuild, GuildID: g, Type: "PRESENCE_UPDATE", Data: data})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // also runs after the connection's context ended
	defer cancel()
	friends, err := h.Social.FriendIDs(ctx, userID)
	if err != nil {
		slog.Warn("gateway: loading friends for presence", "err", err)
		return
	}
	h.Publish(event.Event{Kind: event.ToUsers, UserIDs: friends, Type: "PRESENCE_UPDATE", Data: data})
}
