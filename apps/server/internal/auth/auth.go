// Package auth signs users in through a third-party identity provider and manages sessions.
//
// Web flow: the browser goes to Begin's URL; the provider calls back, and Callback creates the
// session. The HTTP layer carries the flow state in a cookie and the session token in another.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"discordo/server/internal/errs"
	"discordo/server/internal/id"
	"discordo/server/internal/user"
)

// SessionTTL is how long a session lasts.
const SessionTTL = 30 * 24 * time.Hour

// SessionCookie holds the session token in the browser.
const SessionCookie = "discordo_session"

type Identity struct {
	ProviderID  string
	Username    string
	DisplayName string
	AvatarURL   string
}

type Provider interface {
	AuthURL(state, verifier string) string
	Identity(ctx context.Context, code, verifier string) (Identity, error)
}

type Store interface {
	UpsertUser(ctx context.Context, newID string, idn Identity) (user.User, error)
	CreateSession(ctx context.Context, hash []byte, userID string, expires time.Time) error
	SessionUser(ctx context.Context, hash []byte, now time.Time) (user.User, error)
	DeleteSession(ctx context.Context, hash []byte) error
}

type Service struct {
	store    Store
	provider Provider
}

func NewService(store Store, provider Provider) *Service {
	return &Service{store: store, provider: provider}
}

// Begin starts a login. state and verifier must come back unchanged in Callback (the HTTP layer keeps them in a cookie).
func (s *Service) Begin() (url, state, verifier string) {
	state, verifier = random(), random()
	return s.provider.AuthURL(state, verifier), state, verifier
}

// Callback finishes the login with the provider's code and returns a new session token.
func (s *Service) Callback(ctx context.Context, code, verifier string) (string, user.User, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	idn, err := s.provider.Identity(ctx, code, verifier)
	if err != nil {
		return "", user.User{}, fmt.Errorf("%w: %v", errs.ErrUnauthorized, err)
	}
	u, err := s.store.UpsertUser(ctx, id.New(), idn)
	if err != nil {
		return "", user.User{}, err
	}
	token := random()
	return token, u, s.store.CreateSession(ctx, hash(token), u.ID, time.Now().Add(SessionTTL))
}

func (s *Service) Authenticate(ctx context.Context, token string) (user.User, error) {
	if token == "" {
		return user.User{}, errs.ErrUnauthorized
	}
	u, err := s.store.SessionUser(ctx, hash(token), time.Now())
	if errors.Is(err, errs.ErrNotFound) {
		return u, errs.ErrUnauthorized
	}
	return u, err
}

func (s *Service) Logout(ctx context.Context, token string) error {
	return s.store.DeleteSession(ctx, hash(token))
}

func random() string {
	b := make([]byte, 32)
	rand.Read(b) // never fails (panics on a broken system RNG)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Tokens are stored hashed so a database leak does not leak sessions.
func hash(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}
