// Package auth signs users in through a third-party identity provider and manages sessions.
//
// Desktop flow: the app opens the browser at Begin's URL carrying a PKCE challenge; after the
// provider calls back, the browser is redirected to the app with a one-time login code, which
// the app exchanges (with the PKCE verifier) for a session token. The verifier stops another
// local app that hijacks the custom URL scheme from using the code.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"discordo/server/internal/errs"
	"discordo/server/internal/id"
	"discordo/server/internal/user"
)

const (
	loginCodeTTL = time.Minute
	sessionTTL   = 30 * 24 * time.Hour
)

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
	SaveLoginCode(ctx context.Context, hash []byte, userID, challenge string, expires time.Time) error
	// TakeLoginCode deletes the code and returns what it was bound to.
	TakeLoginCode(ctx context.Context, hash []byte, now time.Time) (userID, challenge string, err error)
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
func (s *Service) Begin(challenge string) (url, state, verifier string, err error) {
	if b, err := base64.RawURLEncoding.DecodeString(challenge); err != nil || len(b) != sha256.Size {
		return "", "", "", errs.Invalid("challenge must be base64url(sha256(verifier))")
	}
	state, verifier = random(), random()
	return s.provider.AuthURL(state, verifier), state, verifier, nil
}

// Callback finishes the provider side and returns the one-time login code for the app.
func (s *Service) Callback(ctx context.Context, code, verifier, challenge string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	idn, err := s.provider.Identity(ctx, code, verifier)
	if err != nil {
		return "", fmt.Errorf("%w: %v", errs.ErrUnauthorized, err)
	}
	u, err := s.store.UpsertUser(ctx, id.New(), idn)
	if err != nil {
		return "", err
	}
	loginCode := random()
	return loginCode, s.store.SaveLoginCode(ctx, hash(loginCode), u.ID, challenge, time.Now().Add(loginCodeTTL))
}

// Exchange trades the login code plus the PKCE verifier for a session token.
func (s *Service) Exchange(ctx context.Context, loginCode, verifier string) (string, user.User, error) {
	userID, challenge, err := s.store.TakeLoginCode(ctx, hash(loginCode), time.Now())
	if errors.Is(err, errs.ErrNotFound) {
		return "", user.User{}, errs.ErrUnauthorized
	} else if err != nil {
		return "", user.User{}, err
	}
	got := base64.RawURLEncoding.EncodeToString(hash(verifier))
	if subtle.ConstantTimeCompare([]byte(got), []byte(challenge)) != 1 {
		return "", user.User{}, errs.ErrUnauthorized
	}
	token := random()
	if err := s.store.CreateSession(ctx, hash(token), userID, time.Now().Add(sessionTTL)); err != nil {
		return "", user.User{}, err
	}
	u, err := s.Authenticate(ctx, token)
	return token, u, err
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
