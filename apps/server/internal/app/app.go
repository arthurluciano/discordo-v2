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
	"discordo/server/internal/message"
	"discordo/server/internal/postgres"
	"discordo/server/internal/relation"
)

type Options struct {
	PublicURL string
	Blobs     message.Blobs
}

// New returns the HTTP handler and the hub, whose Run the caller must start.
func New(store *postgres.Store, provider auth.Provider, opt Options) (http.Handler, *gateway.Hub) {
	authSvc := auth.NewService(store, provider)
	hub := gateway.NewHub(authSvc)
	guilds := guild.NewService(store, hub, hub)
	relations := relation.NewService(store, hub, hub)
	dms := dm.NewService(store, relations, hub)
	hub.Guilds = guilds
	hub.Social = social{relations, dms}

	api := &httpapi.API{
		Auth:          authSvc,
		Guilds:        guilds,
		Messages:      message.NewService(store, opt.Blobs, channels{guilds, dms}, hub),
		Relations:     relations,
		DMs:           dms,
		Gateway:       hub,
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

// social is the gateway's view of a user's DMs and relationships.
type social struct {
	relations *relation.Service
	dms       *dm.Service
}

func (s social) Private(ctx context.Context, userID string) (map[string]any, error) {
	rels, err := s.relations.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	dms, err := s.dms.List(ctx, userID)
	return map[string]any{"relationships": rels, "dms": dms}, err
}

func (s social) FriendIDs(ctx context.Context, userID string) ([]string, error) {
	return s.relations.FriendIDs(ctx, userID)
}
