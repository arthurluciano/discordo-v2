package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"discordo/server/internal/dm"
	"discordo/server/internal/relation"
	"discordo/server/internal/user"
)

// Users

func (s *Store) User(ctx context.Context, userID string) (u user.User, err error) {
	err = s.db.QueryRow(ctx, `select id, username, display_name, avatar_url from users where id = $1`, userID).
		Scan(&u.ID, &u.Username, &u.DisplayName, &u.AvatarURL)
	return u, mapErr(err)
}

func (s *Store) UserByUsername(ctx context.Context, username string) (u user.User, err error) {
	err = s.db.QueryRow(ctx, `select id, username, display_name, avatar_url from users where username = $1 order by id limit 1`, username).
		Scan(&u.ID, &u.Username, &u.DisplayName, &u.AvatarURL)
	return u, mapErr(err)
}

// Relationships

func (s *Store) Relationships(ctx context.Context, userID string) ([]relation.Relationship, error) {
	rows, _ := s.db.Query(ctx, `
		select u.id, u.username, u.display_name, u.avatar_url, r.status
		from relationships r join users u on u.id = r.target_id
		where r.user_id = $1 order by u.username`, userID)
	rels, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (r relation.Relationship, err error) {
		err = row.Scan(&r.User.ID, &r.User.Username, &r.User.DisplayName, &r.User.AvatarURL, &r.Status)
		return
	})
	return rels, mapErr(err)
}

func (s *Store) RelationStatus(ctx context.Context, userID, targetID string) (st relation.Status, err error) {
	err = s.db.QueryRow(ctx, `
		select coalesce((select status from relationships where user_id = $1 and target_id = $2), 0)`,
		userID, targetID).Scan(&st)
	return st, mapErr(err)
}

func (s *Store) SetRelations(ctx context.Context, userID, targetID string, mine, theirs relation.Status) error {
	// One statement, so both directions change together. The target's own block (status 4) is never touched.
	return exec(ctx, s.db, `
		with gone as (
			delete from relationships
			where (user_id = $1 and target_id = $2 and $3::int = 0)
			   or (user_id = $2 and target_id = $1 and $4::int = 0 and status <> 4)
		)
		insert into relationships (user_id, target_id, status)
		select u, t, s from (values ($1::uuid, $2::uuid, $3::int), ($2::uuid, $1::uuid, $4::int)) v(u, t, s)
		where s <> 0 and not (u = $2::uuid and exists(
			select 1 from relationships where user_id = $2 and target_id = $1 and status = 4))
		on conflict (user_id, target_id) do update set status = excluded.status`,
		userID, targetID, mine, theirs)
}

func (s *Store) AnyBlock(ctx context.Context, a, b string) (blocked bool, err error) {
	err = s.db.QueryRow(ctx, `
		select exists(select 1 from relationships where status = 4
			and ((user_id = $1 and target_id = $2) or (user_id = $2 and target_id = $1)))`, a, b).Scan(&blocked)
	return blocked, mapErr(err)
}

// DMs

const dmSQL = `
	select c.id, c.type, c.name, (
		select json_agg(json_build_object('id', u.id, 'username', u.username, 'display_name', u.display_name, 'avatar_url', u.avatar_url) order by u.id)
		from dm_members d join users u on u.id = d.user_id where d.channel_id = c.id)
	from channels c where c.guild_id is null `

func scanDM(row pgx.CollectableRow) (c dm.Channel, err error) {
	err = row.Scan(&c.ID, &c.Type, &c.Name, &c.Recipients)
	return
}

func (s *Store) OpenDM(ctx context.Context, newID, a, b string) (channelID string, err error) {
	key := min(a, b) + ":" + max(a, b)
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `insert into channels (id, type, name, dm_key) values ($1, $2, '', $3) on conflict (dm_key) do nothing`,
			newID, dm.TypeDM, key)
		if err != nil {
			return err
		}
		if tag.RowsAffected() > 0 {
			if _, err := tx.Exec(ctx, `insert into dm_members values ($1, $2), ($1, $3)`, newID, a, b); err != nil {
				return err
			}
		}
		return tx.QueryRow(ctx, `select id from channels where dm_key = $1`, key).Scan(&channelID)
	})
	return channelID, mapErr(err)
}

func (s *Store) CreateGroup(ctx context.Context, channelID, name string, userIDs []string) error {
	return mapErr(pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `insert into channels (id, type, name) values ($1, $2, $3)`, channelID, dm.TypeGroup, name); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `insert into dm_members select $1, unnest($2::text[]::uuid[])`, channelID, userIDs)
		return err
	}))
}

func (s *Store) DM(ctx context.Context, channelID string) (dm.Channel, error) {
	rows, _ := s.db.Query(ctx, dmSQL+`and c.id = $1`, channelID)
	c, err := pgx.CollectExactlyOneRow(rows, scanDM)
	return c, mapErr(err)
}

func (s *Store) UserDMs(ctx context.Context, userID string) ([]dm.Channel, error) {
	rows, _ := s.db.Query(ctx, dmSQL+`
		and exists(select 1 from dm_members d where d.channel_id = c.id and d.user_id = $1)
		order by c.id desc`, userID)
	cs, err := pgx.CollectRows(rows, scanDM)
	return cs, mapErr(err)
}

func (s *Store) AddRecipients(ctx context.Context, channelID string, userIDs []string) error {
	return exec(ctx, s.db, `insert into dm_members select $1, unnest($2::uuid[]) on conflict do nothing`, channelID, userIDs)
}

func (s *Store) RemoveRecipient(ctx context.Context, channelID, userID string) error {
	return exec(ctx, s.db, `
		with gone as (delete from dm_members where channel_id = $1 and user_id = $2)
		delete from channels where id = $1
			and not exists(select 1 from dm_members where channel_id = $1 and user_id <> $2)`, channelID, userID)
}
