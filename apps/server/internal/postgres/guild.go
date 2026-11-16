package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"discordo/server/internal/errs"
	"discordo/server/internal/guild"
	"discordo/server/internal/permission"
)

func (s *Store) CreateGuild(ctx context.Context, g guild.Guild, everyone guild.Role, channels []guild.Channel) error {
	return mapErr(pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `insert into guilds (id, name, icon_url, owner_id) values ($1, $2, $3, $4)`,
			g.ID, g.Name, g.IconURL, g.OwnerID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, saveRoleSQL, everyone.ID, everyone.GuildID, everyone.Name, everyone.Color, everyone.Permissions, everyone.Position); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `insert into members (guild_id, user_id) values ($1, $2)`, g.ID, g.OwnerID); err != nil {
			return err
		}
		for _, c := range channels {
			if _, err := tx.Exec(ctx, saveChannelSQL, c.ID, c.GuildID, c.Type, c.Name, c.Topic, c.Position); err != nil {
				return err
			}
		}
		return nil
	}))
}

func (s *Store) Guild(ctx context.Context, guildID string) (g guild.Guild, err error) {
	err = s.db.QueryRow(ctx, `select id, name, icon_url, banner_url, owner_id from guilds where id = $1`, guildID).
		Scan(&g.ID, &g.Name, &g.IconURL, &g.BannerURL, &g.OwnerID)
	return g, mapErr(err)
}

func (s *Store) UpdateGuild(ctx context.Context, g guild.Guild) error {
	return execOne(ctx, s.db, `update guilds set name = $2, icon_url = $3, banner_url = $4 where id = $1`, g.ID, g.Name, g.IconURL, g.BannerURL)
}

func (s *Store) DeleteGuild(ctx context.Context, guildID string) error {
	return execOne(ctx, s.db, `delete from guilds where id = $1`, guildID)
}

func (s *Store) UserGuildIDs(ctx context.Context, userID string) ([]string, error) {
	rows, _ := s.db.Query(ctx, `select guild_id from members where user_id = $1 order by joined_at`, userID)
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	return ids, mapErr(err)
}

// Roles

const saveRoleSQL = `
	insert into roles (id, guild_id, name, color, permissions, position) values ($1, $2, $3, $4, $5, $6)
	on conflict (id) do update set name = excluded.name, color = excluded.color,
		permissions = excluded.permissions, position = excluded.position`

func (s *Store) Roles(ctx context.Context, guildID string) ([]guild.Role, error) {
	rows, _ := s.db.Query(ctx, `
		select id, guild_id, name, color, permissions, position from roles
		where guild_id = $1 order by position desc, id`, guildID)
	roles, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (r guild.Role, err error) {
		err = row.Scan(&r.ID, &r.GuildID, &r.Name, &r.Color, &r.Permissions, &r.Position)
		return
	})
	return roles, mapErr(err)
}

func (s *Store) SaveRole(ctx context.Context, r guild.Role) error {
	return exec(ctx, s.db, saveRoleSQL, r.ID, r.GuildID, r.Name, r.Color, r.Permissions, r.Position)
}

func (s *Store) DeleteRole(ctx context.Context, roleID string) error {
	return execOne(ctx, s.db, `delete from roles where id = $1`, roleID)
}

// Members

const memberSQL = `
	select u.id, u.username, u.display_name, u.avatar_url, m.nickname, m.joined_at,
		coalesce(array_agg(mr.role_id::text) filter (where mr.role_id is not null), '{}')
	from members m
	join users u on u.id = m.user_id
	left join member_roles mr on mr.guild_id = m.guild_id and mr.user_id = m.user_id
	where m.guild_id = $1 and ($2::uuid is null or m.user_id = $2)
	group by u.id, m.guild_id, m.user_id
	order by m.joined_at`

func scanMember(row pgx.CollectableRow) (m guild.Member, err error) {
	err = row.Scan(&m.User.ID, &m.User.Username, &m.User.DisplayName, &m.User.AvatarURL, &m.Nickname, &m.JoinedAt, &m.RoleIDs)
	return
}

func (s *Store) Members(ctx context.Context, guildID string) ([]guild.Member, error) {
	rows, _ := s.db.Query(ctx, memberSQL, guildID, nil)
	members, err := pgx.CollectRows(rows, scanMember)
	return members, mapErr(err)
}

func (s *Store) Member(ctx context.Context, guildID, userID string) (guild.Member, error) {
	rows, _ := s.db.Query(ctx, memberSQL, guildID, userID)
	m, err := pgx.CollectExactlyOneRow(rows, scanMember)
	return m, mapErr(err)
}

func (s *Store) RemoveMember(ctx context.Context, guildID, userID string) error {
	return execOne(ctx, s.db, `delete from members where guild_id = $1 and user_id = $2`, guildID, userID)
}

func (s *Store) SetMemberRole(ctx context.Context, guildID, userID, roleID string, on bool) error {
	if !on {
		return exec(ctx, s.db, `delete from member_roles where guild_id = $1 and user_id = $2 and role_id = $3`, guildID, userID, roleID)
	}
	return exec(ctx, s.db, `insert into member_roles values ($1, $2, $3) on conflict do nothing`, guildID, userID, roleID)
}

func (s *Store) Ban(ctx context.Context, guildID, userID string) (wasMember bool, err error) {
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `insert into bans (guild_id, user_id) values ($1, $2) on conflict do nothing`, guildID, userID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `delete from members where guild_id = $1 and user_id = $2`, guildID, userID)
		wasMember = tag.RowsAffected() > 0
		return err
	})
	return wasMember, mapErr(err)
}

func (s *Store) Unban(ctx context.Context, guildID, userID string) error {
	return execOne(ctx, s.db, `delete from bans where guild_id = $1 and user_id = $2`, guildID, userID)
}

// Channels

const (
	channelSQL = `
		select c.id, c.guild_id, c.type, c.name, c.topic, c.position, coalesce((
			select json_agg(json_build_object('target_id', o.target_id, 'is_member', o.is_member, 'allow', o.allow, 'deny', o.deny))
			from channel_overwrites o where o.channel_id = c.id), '[]'::json)
		from channels c `
	saveChannelSQL = `
		insert into channels (id, guild_id, type, name, topic, position) values ($1, $2, $3, $4, $5, $6)
		on conflict (id) do update set name = excluded.name, topic = excluded.topic, position = excluded.position`
)

func scanChannel(row pgx.CollectableRow) (c guild.Channel, err error) {
	err = row.Scan(&c.ID, &c.GuildID, &c.Type, &c.Name, &c.Topic, &c.Position, &c.Overwrites)
	return
}

func (s *Store) Channels(ctx context.Context, guildID string) ([]guild.Channel, error) {
	rows, _ := s.db.Query(ctx, channelSQL+`where c.guild_id = $1 order by c.position, c.id`, guildID)
	channels, err := pgx.CollectRows(rows, scanChannel)
	return channels, mapErr(err)
}

func (s *Store) Channel(ctx context.Context, channelID string) (guild.Channel, error) {
	rows, _ := s.db.Query(ctx, channelSQL+`where c.id = $1 and c.guild_id is not null`, channelID)
	c, err := pgx.CollectExactlyOneRow(rows, scanChannel)
	return c, mapErr(err)
}

func (s *Store) SaveChannel(ctx context.Context, c guild.Channel) error {
	return exec(ctx, s.db, saveChannelSQL, c.ID, c.GuildID, c.Type, c.Name, c.Topic, c.Position)
}

func (s *Store) DeleteChannel(ctx context.Context, channelID string) error {
	return execOne(ctx, s.db, `delete from channels where id = $1`, channelID)
}

func (s *Store) SetOverwrite(ctx context.Context, channelID string, o permission.Overwrite) error {
	return exec(ctx, s.db, `
		insert into channel_overwrites values ($1, $2, $3, $4, $5)
		on conflict (channel_id, target_id) do update set allow = excluded.allow, deny = excluded.deny`,
		channelID, o.TargetID, o.IsMember, o.Allow, o.Deny)
}

func (s *Store) DeleteOverwrite(ctx context.Context, channelID, targetID string) error {
	return execOne(ctx, s.db, `delete from channel_overwrites where channel_id = $1 and target_id = $2`, channelID, targetID)
}

// Invites

func (s *Store) CreateInvite(ctx context.Context, inv guild.Invite) error {
	return exec(ctx, s.db, `insert into invites (code, guild_id, creator_id, max_uses, expires_at) values ($1, $2, $3, $4, $5)`,
		inv.Code, inv.GuildID, inv.CreatorID, inv.MaxUses, inv.ExpiresAt)
}

func (s *Store) UseInvite(ctx context.Context, code, userID string, now time.Time) (guildID string, joined bool, err error) {
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		// "for update" serializes concurrent joins so max_uses cannot be exceeded.
		if err := tx.QueryRow(ctx, `
			select guild_id from invites
			where code = $1 and (expires_at is null or expires_at > $2) and (max_uses = 0 or uses < max_uses)
			for update`, code, now).Scan(&guildID); err != nil {
			return err
		}
		var banned bool
		if err := tx.QueryRow(ctx, `select exists(select 1 from bans where guild_id = $1 and user_id = $2)`, guildID, userID).Scan(&banned); err != nil {
			return err
		}
		if banned {
			return errs.ErrForbidden
		}
		tag, err := tx.Exec(ctx, `insert into members (guild_id, user_id) values ($1, $2) on conflict do nothing`, guildID, userID)
		if err != nil {
			return err
		}
		if joined = tag.RowsAffected() > 0; joined {
			_, err = tx.Exec(ctx, `update invites set uses = uses + 1 where code = $1`, code)
		}
		return err
	})
	return guildID, joined, mapErr(err)
}
