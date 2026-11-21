// Package relation is friends, friend requests and blocks.
package relation

import (
	"context"
	"strings"

	"discordo/server/internal/errs"
	"discordo/server/internal/event"
	"discordo/server/internal/user"
)

type Status int

const (
	None Status = iota
	Friend
	PendingIn
	PendingOut
	Blocked
)

type Relationship struct {
	User   user.User `json:"user"`
	Status Status    `json:"status"`
	Online bool      `json:"online"` // only reported for friends
}

type Store interface {
	User(ctx context.Context, userID string) (user.User, error)
	UserByUsername(ctx context.Context, username string) (user.User, error)
	Relationships(ctx context.Context, userID string) ([]Relationship, error)
	RelationStatus(ctx context.Context, userID, targetID string) (Status, error)
	// SetRelations atomically sets both directions (None deletes). It never touches a Blocked row of the target.
	SetRelations(ctx context.Context, userID, targetID string, mine, theirs Status) error
	AnyBlock(ctx context.Context, a, b string) (bool, error)
}

type Presence interface {
	Online(userID string) bool
}

type Service struct {
	store    Store
	pub      event.Publisher
	presence Presence
}

func NewService(store Store, pub event.Publisher, presence Presence) *Service {
	return &Service{store: store, pub: pub, presence: presence}
}

func (s *Service) List(ctx context.Context, userID string) ([]Relationship, error) {
	rels, err := s.store.Relationships(ctx, userID)
	for i, r := range rels {
		rels[i].Online = r.Status == Friend && s.presence.Online(r.User.ID)
	}
	return rels, err
}

func (s *Service) FriendIDs(ctx context.Context, userID string) ([]string, error) {
	rels, err := s.store.Relationships(ctx, userID)
	var ids []string
	for _, r := range rels {
		if r.Status == Friend {
			ids = append(ids, r.User.ID)
		}
	}
	return ids, err
}

func (s *Service) AreFriends(ctx context.Context, a, b string) (bool, error) {
	st, err := s.store.RelationStatus(ctx, a, b)
	return st == Friend, err
}

// Blocked reports a block in either direction.
func (s *Service) Blocked(ctx context.Context, a, b string) (bool, error) {
	return s.store.AnyBlock(ctx, a, b)
}

// Request sends a friend request by username, or accepts the one already pending from that user.
func (s *Service) Request(ctx context.Context, userID, username string) error {
	target, err := s.store.UserByUsername(ctx, strings.TrimSpace(username))
	if err != nil {
		return err
	}
	if target.ID == userID {
		return errs.Invalid("you cannot add yourself")
	}
	mine, err := s.store.RelationStatus(ctx, userID, target.ID)
	if err != nil {
		return err
	}
	switch mine {
	case Friend, PendingOut:
		return nil
	case Blocked:
		return errs.Invalid("unblock this user first")
	case PendingIn:
		return s.set(ctx, userID, target.ID, Friend, Friend)
	}
	return s.set(ctx, userID, target.ID, PendingOut, PendingIn)
}

func (s *Service) Accept(ctx context.Context, userID, targetID string) error {
	if mine, err := s.store.RelationStatus(ctx, userID, targetID); err != nil {
		return err
	} else if mine != PendingIn {
		return errs.ErrNotFound
	}
	return s.set(ctx, userID, targetID, Friend, Friend)
}

// Remove unfriends, declines, cancels or unblocks, depending on the current state.
func (s *Service) Remove(ctx context.Context, userID, targetID string) error {
	return s.set(ctx, userID, targetID, None, None)
}

func (s *Service) Block(ctx context.Context, userID, targetID string) error {
	if userID == targetID {
		return errs.Invalid("you cannot block yourself")
	}
	return s.set(ctx, userID, targetID, Blocked, None)
}

func (s *Service) set(ctx context.Context, userID, targetID string, mine, theirs Status) error {
	me, err := s.store.User(ctx, userID)
	if err != nil {
		return err
	}
	target, err := s.store.User(ctx, targetID)
	if err != nil {
		return err
	}
	theirsBefore, err := s.store.RelationStatus(ctx, targetID, userID)
	if err != nil {
		return err
	}
	if theirsBefore == Blocked && mine != None && mine != Blocked {
		return errs.ErrNotFound // to someone they blocked, the user looks nonexistent
	}
	if err := s.store.SetRelations(ctx, userID, targetID, mine, theirs); err != nil {
		return err
	}
	s.notify(userID, target, mine)
	if theirsBefore != Blocked { // their block stays as it is, and they learn nothing
		s.notify(targetID, me, theirs)
	}
	return nil
}

func (s *Service) notify(userID string, other user.User, status Status) {
	s.pub.Publish(event.Event{Kind: event.ToUsers, UserIDs: []string{userID}, Type: "RELATIONSHIP_UPDATE",
		Data: Relationship{User: other, Status: status, Online: status == Friend && s.presence.Online(other.ID)}})
}
