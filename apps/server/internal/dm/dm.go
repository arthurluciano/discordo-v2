// Package dm is direct messages: 1:1 conversations and group DMs. They are channels without a guild.
package dm

import (
	"context"
	"slices"
	"strings"
	"unicode/utf8"

	"discordo/server/internal/errs"
	"discordo/server/internal/event"
	"discordo/server/internal/id"
	"discordo/server/internal/permission"
	"discordo/server/internal/user"
)

const (
	TypeDM    = 2
	TypeGroup = 3

	maxGroupSize = 10
	perms        = permission.ViewChannel | permission.SendMessages | permission.AttachFiles |
		permission.AddReactions | permission.Connect | permission.Speak | permission.Stream
)

type Channel struct {
	ID         string      `json:"id"`
	Type       int         `json:"type"`
	Name       string      `json:"name"`
	Recipients []user.User `json:"recipients"`
}

func (c Channel) recipientIDs() []string {
	ids := make([]string, len(c.Recipients))
	for i, u := range c.Recipients {
		ids[i] = u.ID
	}
	return ids
}

type Store interface {
	// OpenDM returns the 1:1 channel of the pair, creating it with newID if there is none.
	OpenDM(ctx context.Context, newID, a, b string) (channelID string, err error)
	CreateGroup(ctx context.Context, channelID, name string, userIDs []string) error
	DM(ctx context.Context, channelID string) (Channel, error)
	UserDMs(ctx context.Context, userID string) ([]Channel, error)
	AddRecipient(ctx context.Context, channelID, userID string) error
	// RemoveRecipient deletes the channel when nobody is left.
	RemoveRecipient(ctx context.Context, channelID, userID string) error
}

type Relations interface {
	Blocked(ctx context.Context, a, b string) (bool, error)
	AreFriends(ctx context.Context, a, b string) (bool, error)
}

type Service struct {
	store     Store
	relations Relations
	pub       event.Publisher
}

func NewService(store Store, relations Relations, pub event.Publisher) *Service {
	return &Service{store: store, relations: relations, pub: pub}
}

func (s *Service) List(ctx context.Context, userID string) ([]Channel, error) {
	return s.store.UserDMs(ctx, userID)
}

// Open returns the 1:1 conversation with target, creating it on first use.
// shortcut: anyone may DM anyone who has not blocked them; require a shared guild or friendship if spam appears.
func (s *Service) Open(ctx context.Context, userID, targetID string) (Channel, error) {
	if userID == targetID {
		return Channel{}, errs.Invalid("you cannot DM yourself")
	}
	if blocked, err := s.relations.Blocked(ctx, userID, targetID); err != nil {
		return Channel{}, err
	} else if blocked {
		return Channel{}, errs.ErrForbidden
	}
	channelID, err := s.store.OpenDM(ctx, id.New(), userID, targetID)
	if err != nil {
		return Channel{}, err
	}
	return s.announce(ctx, channelID, "DM_CREATE")
}

func (s *Service) CreateGroup(ctx context.Context, userID, name string, recipientIDs []string) (Channel, error) {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > 100 {
		return Channel{}, errs.Invalid("name must have at most 100 characters")
	}
	members := []string{userID}
	for _, r := range recipientIDs {
		if !id.Valid(r) {
			return Channel{}, errs.Invalid("recipient_ids must be user IDs")
		}
		if slices.Contains(members, r) {
			continue
		}
		if err := s.requireFriend(ctx, userID, r); err != nil {
			return Channel{}, err
		}
		members = append(members, r)
	}
	if len(members) < 2 || len(members) > maxGroupSize {
		return Channel{}, errs.Invalid("a group has 2 to 10 people")
	}
	channelID := id.New()
	if err := s.store.CreateGroup(ctx, channelID, name, members); err != nil {
		return Channel{}, err
	}
	return s.announce(ctx, channelID, "DM_CREATE")
}

func (s *Service) AddRecipient(ctx context.Context, userID, channelID, targetID string) error {
	c, err := s.member(ctx, userID, channelID)
	if err != nil {
		return err
	}
	if c.Type != TypeGroup || len(c.Recipients) >= maxGroupSize {
		return errs.Invalid("only groups with fewer than 10 people accept new members")
	}
	if err := s.requireFriend(ctx, userID, targetID); err != nil {
		return err
	}
	if err := s.store.AddRecipient(ctx, channelID, targetID); err != nil {
		return err
	}
	_, err = s.announce(ctx, channelID, "DM_UPDATE")
	return err
}

func (s *Service) Leave(ctx context.Context, userID, channelID string) error {
	c, err := s.member(ctx, userID, channelID)
	if err != nil {
		return err
	}
	if c.Type != TypeGroup {
		return errs.Invalid("only groups can be left")
	}
	if err := s.store.RemoveRecipient(ctx, channelID, userID); err != nil {
		return err
	}
	s.pub.Publish(event.Event{Kind: event.ToUsers, UserIDs: []string{userID}, Type: "DM_DELETE", Data: map[string]string{"id": channelID}})
	if len(c.Recipients) > 1 {
		_, err = s.announce(ctx, channelID, "DM_UPDATE")
	}
	return err
}

// Access returns what the user may do in a DM channel and who receives its events.
// ErrNotFound if it is not a DM or the user is not in it.
func (s *Service) Access(ctx context.Context, userID, channelID string) (permission.Set, []string, error) {
	c, err := s.member(ctx, userID, channelID)
	if err != nil {
		return 0, nil, err
	}
	p := permission.Set(perms)
	if c.Type == TypeDM {
		other := c.Recipients[0].ID
		if other == userID && len(c.Recipients) > 1 {
			other = c.Recipients[1].ID
		}
		if blocked, err := s.relations.Blocked(ctx, userID, other); err != nil {
			return 0, nil, err
		} else if blocked {
			p = permission.ViewChannel // history stays readable, nothing new gets through
		}
	}
	return p, c.recipientIDs(), nil
}

func (s *Service) member(ctx context.Context, userID, channelID string) (Channel, error) {
	c, err := s.store.DM(ctx, channelID)
	if err != nil {
		return c, err
	}
	if !slices.ContainsFunc(c.Recipients, func(u user.User) bool { return u.ID == userID }) {
		return c, errs.ErrNotFound
	}
	return c, nil
}

func (s *Service) requireFriend(ctx context.Context, userID, targetID string) error {
	if ok, err := s.relations.AreFriends(ctx, userID, targetID); err != nil {
		return err
	} else if !ok {
		return errs.ErrForbidden
	}
	return nil
}

func (s *Service) announce(ctx context.Context, channelID, typ string) (Channel, error) {
	c, err := s.store.DM(ctx, channelID)
	if err != nil {
		return c, err
	}
	s.pub.Publish(event.Event{Kind: event.ToUsers, UserIDs: c.recipientIDs(), Type: typ, Data: c})
	return c, nil
}
