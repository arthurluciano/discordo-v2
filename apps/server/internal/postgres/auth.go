package postgres

import (
	"context"
	"time"

	"discordo/server/internal/auth"
	"discordo/server/internal/user"
)

func (s *Store) UpsertUser(ctx context.Context, newID string, idn auth.Identity) (u user.User, err error) {
	// A name or avatar the user set in Discordo wins over what Discord says.
	err = s.db.QueryRow(ctx, `
		insert into users (id, discord_id, username, display_name, avatar_url) values ($1, $2, $3, $4, $5)
		on conflict (discord_id) do update set
			username = excluded.username,
			display_name = case when users.custom_name then users.display_name else excluded.display_name end,
			avatar_url = case when users.custom_avatar then users.avatar_url else excluded.avatar_url end
		returning id, username, display_name, avatar_url`,
		newID, idn.ProviderID, idn.Username, idn.DisplayName, idn.AvatarURL,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.AvatarURL)
	return u, mapErr(err)
}

// UpdateProfile sets the user's own name and avatar; from then on logins no longer overwrite them.
func (s *Store) UpdateProfile(ctx context.Context, u user.User) error {
	return execOne(ctx, s.db, `
		update users set display_name = $2, avatar_url = $3, custom_name = true, custom_avatar = true where id = $1`,
		u.ID, u.DisplayName, u.AvatarURL)
}

func (s *Store) Details(ctx context.Context, userID string) (d user.Details, err error) {
	err = s.db.QueryRow(ctx, `select bio, pronouns, banner_color, created_at from users where id = $1`, userID).
		Scan(&d.Bio, &d.Pronouns, &d.BannerColor, &d.CreatedAt)
	return d, mapErr(err)
}

func (s *Store) UpdateDetails(ctx context.Context, userID string, d user.Details) error {
	return execOne(ctx, s.db, `update users set bio = $2, pronouns = $3, banner_color = $4 where id = $1`,
		userID, d.Bio, d.Pronouns, d.BannerColor)
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
