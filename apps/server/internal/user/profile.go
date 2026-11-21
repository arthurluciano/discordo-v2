package user

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"discordo/server/internal/errs"
	"discordo/server/internal/event"
	"discordo/server/internal/image"
)

type Store interface {
	User(ctx context.Context, userID string) (User, error)
	UpdateProfile(ctx context.Context, u User) error
	Details(ctx context.Context, userID string) (Details, error)
	UpdateDetails(ctx context.Context, userID string, d Details) error
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
	Bio         *string `json:"bio"`
	Pronouns    *string `json:"pronouns"`
	BannerColor *int    `json:"banner_color"`
}

const (
	maxBio      = 190
	maxPronouns = 40
)

// Profile is a user's card as the viewer may see it: their own, or someone they share a server, a friendship
// or a DM with. Anyone else gets not found, as if the user did not exist.
func (s *Service) Profile(ctx context.Context, viewerID, userID string) (Profile, error) {
	p := Profile{MutualGuildIDs: []string{}}

	if viewerID != userID {
		mutual, visible, err := s.shared(ctx, viewerID, userID)
		if err != nil {
			return p, err
		}
		if !visible {
			return p, errs.ErrNotFound
		}
		p.MutualGuildIDs = mutual
	}

	u, err := s.store.User(ctx, userID)
	if err != nil {
		return p, err
	}
	d, err := s.store.Details(ctx, userID)
	if err != nil {
		return p, err
	}

	p.User, p.Details = u, d
	return p, nil
}

// shared returns the servers both users are in, and whether the viewer may see the other at all.
func (s *Service) shared(ctx context.Context, viewerID, userID string) ([]string, bool, error) {
	mine, err := s.audience.UserGuildIDs(ctx, viewerID)
	if err != nil {
		return nil, false, err
	}
	theirs, err := s.audience.UserGuildIDs(ctx, userID)
	if err != nil {
		return nil, false, err
	}

	in := make(map[string]bool, len(mine))
	for _, g := range mine {
		in[g] = true
	}
	mutual := []string{}
	for _, g := range theirs {
		if in[g] {
			mutual = append(mutual, g)
		}
	}
	if len(mutual) > 0 {
		return mutual, true, nil
	}

	contacts, err := s.audience.Contacts(ctx, userID)
	if err != nil {
		return nil, false, err
	}
	return mutual, slices.Contains(contacts, viewerID), nil
}

// updateDetails applies the bio, pronouns and banner color in the input, if any.
func (s *Service) updateDetails(ctx context.Context, userID string, in ProfileInput) error {
	if in.Bio == nil && in.Pronouns == nil && in.BannerColor == nil {
		return nil
	}
	d, err := s.store.Details(ctx, userID)
	if err != nil {
		return err
	}
	if in.Bio != nil {
		bio := strings.TrimSpace(*in.Bio)
		if utf8.RuneCountInString(bio) > maxBio {
			return errs.Invalid(fmt.Sprintf("bio must have at most %d characters", maxBio))
		}
		d.Bio = bio
	}
	if in.Pronouns != nil {
		pronouns := strings.TrimSpace(*in.Pronouns)
		if utf8.RuneCountInString(pronouns) > maxPronouns {
			return errs.Invalid(fmt.Sprintf("pronouns must have at most %d characters", maxPronouns))
		}
		d.Pronouns = pronouns
	}
	if in.BannerColor != nil {
		if *in.BannerColor < 0 || *in.BannerColor > 0xFFFFFF {
			return errs.Invalid("banner_color must be 0xRRGGBB, or 0 for the default")
		}
		d.BannerColor = *in.BannerColor
	}
	return s.store.UpdateDetails(ctx, userID, d)
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
	if err := s.updateDetails(ctx, userID, in); err != nil {
		return u, err
	}
	// Only a new name or avatar marks them as chosen (logging in stops taking Discord's) and is broadcast.
	if in.DisplayName == nil && in.AvatarURL == nil {
		return u, nil
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
