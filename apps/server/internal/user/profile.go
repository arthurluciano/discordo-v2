package user

import (
	"context"
	"strings"
	"unicode/utf8"

	"discordo/server/internal/errs"
	"discordo/server/internal/event"
	"discordo/server/internal/image"
)

type Store interface {
	User(ctx context.Context, userID string) (User, error)
	UpdateProfile(ctx context.Context, u User) error
}

// Audience is who can see a user: the guilds they are in, and the people they share friendships or DMs with.
type Audience interface {
	UserGuildIDs(ctx context.Context, userID string) ([]string, error)
	Contacts(ctx context.Context, userID string) ([]string, error)
}

type Service struct {
	store    Store
	audience Audience
	pub      event.Publisher
}

func NewService(store Store, audience Audience, pub event.Publisher) *Service {
	return &Service{store: store, audience: audience, pub: pub}
}

type ProfileInput struct {
	DisplayName *string `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"`
}

// UpdateProfile changes the user's own name or avatar and tells everyone who can see them (USER_UPDATE).
func (s *Service) UpdateProfile(ctx context.Context, userID string, in ProfileInput) (User, error) {
	u, err := s.store.User(ctx, userID)
	if err != nil {
		return u, err
	}
	if in.DisplayName != nil {
		name := strings.TrimSpace(*in.DisplayName)
		if n := utf8.RuneCountInString(name); n < 1 || n > 32 {
			return u, errs.Invalid("display_name must have 1 to 32 characters")
		}
		u.DisplayName = name
	}
	if in.AvatarURL != nil {
		if !image.Valid(*in.AvatarURL) {
			return u, errs.Invalid("avatar_url must be empty or an uploaded image (POST /images)")
		}
		u.AvatarURL = *in.AvatarURL
	}
	if err := s.store.UpdateProfile(ctx, u); err != nil {
		return u, err
	}

	// Best effort: a client that misses it still gets the new profile on its next READY.
	guildIDs, _ := s.audience.UserGuildIDs(ctx, userID)
	for _, g := range guildIDs {
		s.pub.Publish(event.Event{Kind: event.ToGuild, GuildID: g, Type: "USER_UPDATE", Data: u})
	}
	contacts, _ := s.audience.Contacts(ctx, userID)
	s.pub.Publish(event.Event{Kind: event.ToUsers, UserIDs: append(contacts, userID), Type: "USER_UPDATE", Data: u})
	return u, nil
}
