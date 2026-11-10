package postgres

import (
	"context"
	"time"

	"discordo/server/internal/auth"
	"discordo/server/internal/user"
)

func (s *Store) UpsertUser(ctx context.Context, newID string, idn auth.Identity) (u user.User, err error) {
	err = s.db.QueryRow(ctx, `
		insert into users (id, discord_id, username, display_name, avatar_url) values ($1, $2, $3, $4, $5)
		on conflict (discord_id) do update set
			username = excluded.username, display_name = excluded.display_name, avatar_url = excluded.avatar_url
		returning id, username, display_name, avatar_url`,
		newID, idn.ProviderID, idn.Username, idn.DisplayName, idn.AvatarURL,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.AvatarURL)
	return u, mapErr(err)
}

func (s *Store) SaveLoginCode(ctx context.Context, hash []byte, userID, challenge string, expires time.Time) error {
	return exec(ctx, s.db, `insert into login_codes values ($1, $2, $3, $4)`, hash, userID, challenge, expires)
}

func (s *Store) TakeLoginCode(ctx context.Context, hash []byte, now time.Time) (userID, challenge string, err error) {
	// Expired codes are swept here too, so the table cannot grow without bound.
	err = s.db.QueryRow(ctx, `
		with swept as (delete from login_codes where expires_at <= $2)
		delete from login_codes where code_hash = $1 and expires_at > $2 returning user_id, challenge`,
		hash, now).Scan(&userID, &challenge)
	return userID, challenge, mapErr(err)
}

func (s *Store) CreateSession(ctx context.Context, hash []byte, userID string, expires time.Time) error {
	return exec(ctx, s.db, `
		with swept as (delete from sessions where expires_at <= now())
		insert into sessions values ($1, $2, $3)`, hash, userID, expires)
}

func (s *Store) SessionUser(ctx context.Context, hash []byte, now time.Time) (u user.User, err error) {
	err = s.db.QueryRow(ctx, `
		select u.id, u.username, u.display_name, u.avatar_url
		from sessions s join users u on u.id = s.user_id
		where s.token_hash = $1 and s.expires_at > $2`, hash, now,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.AvatarURL)
	return u, mapErr(err)
}

func (s *Store) DeleteSession(ctx context.Context, hash []byte) error {
	return exec(ctx, s.db, `delete from sessions where token_hash = $1`, hash)
}
