// Package app wires the concrete implementations together. It is the only place that knows all of them.
package app

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"discordo/server/internal/auth"
	"discordo/server/internal/dm"
	"discordo/server/internal/errs"
	"discordo/server/internal/gateway"
	"discordo/server/internal/guild"
	"discordo/server/internal/httpapi"
	"discordo/server/internal/image"
	"discordo/server/internal/media"
	"discordo/server/internal/message"
	"discordo/server/internal/permission"
	"discordo/server/internal/postgres"
	"discordo/server/internal/relation"
	"discordo/server/internal/user"
	"discordo/server/internal/voice"
)

type Options struct {
	PublicURL string
	Blobs     message.Blobs
	Media     media.Provider
	Web       http.Handler // the web app, served for every path the API does not own
}

// New returns the HTTP handler and the hub, whose Run the caller must start.
func New(store *postgres.Store, provider auth.Provider, opt Options) (http.Handler, *gateway.Hub) {
	authSvc := auth.NewService(store, provider)
	hub := gateway.NewHub(authSvc)
	guilds := guild.NewService(store, hub, hub)
	relations := relation.NewService(store, hub, hub)
	dms := dm.NewService(store, relations, hub)
	rooms := channels{guilds, dms}
	calls := voice.NewService(rooms, opt.Media, hub)
	guilds.OnRemoved = calls.DropFromGuild
	hub.Guilds = guilds
	hub.Social = social{relations, dms, rooms, calls}
	hub.Voice = calls

	api := &httpapi.API{
		Auth:          authSvc,
		Guilds:        guilds,
		Messages:      message.NewService(store, opt.Blobs, rooms, hub),
		Relations:     relations,
		DMs:           dms,
		Gateway:       hub,
		Users:         user.NewService(store, audience{store, relations, dms}, hub),
		Images:        image.NewService(opt.Blobs),
		Web:           opt.Web,
		SecureCookies: strings.HasPrefix(opt.PublicURL, "https://"),
	}
	return api.Handler(), hub
}

// channels lets the message service treat guild channels and DMs alike.
type channels struct {
	guilds *guild.Service
	dms    *dm.Service
}

func (c channels) ChannelAccess(ctx context.Context, userID, channelID string) (message.Access, error) {
	perms, guildID, err := c.guilds.ChannelAccess(ctx, userID, channelID)
	if !errors.Is(err, errs.ErrNotFound) {
		return message.Access{Perms: perms, GuildID: guildID}, err
	}
	perms, recipients, err := c.dms.Access(ctx, userID, channelID)
	return message.Access{Perms: perms, Recipients: recipients}, err
}

func (c channels) VoiceAccess(ctx context.Context, userID, channelID string) (permission.Set, string, []string, error) {
	perms, guildID, err := c.guilds.VoiceAccess(ctx, userID, channelID)
	if !errors.Is(err, errs.ErrNotFound) {
		return perms, guildID, nil, err
	}
	perms, recipients, err := c.dms.Access(ctx, userID, channelID) // any DM can host a call
	return perms, "", recipients, err
}

// social is the gateway's view of a user's DMs, relationships and the calls they can see.
type social struct {
	relations *relation.Service
	dms       *dm.Service
	rooms     channels
	calls     *voice.Service
}

func (s social) Private(ctx context.Context, userID string) (map[string]any, error) {
	rels, err := s.relations.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	dms, err := s.dms.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	// Calls in channels this user cannot see stay invisible. Few calls, so checking each is fine.
	visible := []voice.State{}
	for _, st := range s.calls.States() {
		if _, err := s.rooms.ChannelAccess(ctx, userID, st.ChannelID); err == nil {
			visible = append(visible, st)
		}
	}
	return map[string]any{"relationships": rels, "dms": dms, "voice_states": visible}, nil
}

func (s social) FriendIDs(ctx context.Context, userID string) ([]string, error) {
	return s.relations.FriendIDs(ctx, userID)
}

// audience is who sees a user's profile change.
type audience struct {
	store     *postgres.Store
	relations *relation.Service
	dms       *dm.Service
}

func (a audience) UserGuildIDs(ctx context.Context, userID string) ([]string, error) {
	return a.store.UserGuildIDs(ctx, userID)
}

// Contacts are friends plus everyone in a DM with the user.
func (a audience) Contacts(ctx context.Context, userID string) ([]string, error) {
	ids, err := a.relations.FriendIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	dms, err := a.dms.List(ctx, userID)
	for _, c := range dms {
		for _, r := range c.Recipients {
			ids = append(ids, r.ID)
		}
	}
	return ids, err
}
