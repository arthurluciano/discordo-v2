package guild

import (
	"context"

	"discordo/server/internal/errs"
	"discordo/server/internal/permission"
)

// Snapshot is everything one user may see of a guild. Clients replace their copy of the
// guild with it, so structural changes need no fine-grained events.
// shortcut: carries the full member list, fine up to a few thousand members; paginate members and go granular beyond that.
type Snapshot struct {
	Guild
	Permissions permission.Set    `json:"permissions"` // the viewer's guild-level permissions
	Roles       []Role            `json:"roles"`
	Channels    []SnapshotChannel `json:"channels"`
	Members     []Member          `json:"members"`
}

// SnapshotChannel is a channel with what the viewer may do in it, so clients need no permission math.
type SnapshotChannel struct {
	Channel
	Permissions permission.Set `json:"permissions"`
}

type full struct {
	guild   Guild
	roles   []Role
	members []Member
}

func (s *Service) load(ctx context.Context, guildID string) (f full, err error) {
	if f.guild, err = s.store.Guild(ctx, guildID); err != nil {
		return
	}
	if f.roles, err = s.store.Roles(ctx, guildID); err != nil {
		return
	}
	f.members, err = s.store.Members(ctx, guildID)
	return
}

// Snapshot returns any so the gateway does not need this package's types.
func (s *Service) Snapshot(ctx context.Context, userID, guildID string) (any, error) {
	return s.snapshot(ctx, userID, guildID)
}

func (s *Service) snapshot(ctx context.Context, userID, guildID string) (Snapshot, error) {
	f, err := s.load(ctx, guildID)
	if err != nil {
		return Snapshot{}, err
	}
	channels, err := s.store.Channels(ctx, guildID)
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{Guild: f.guild, Roles: f.roles, Members: f.members, Channels: []SnapshotChannel{}}
	found := false
	for i, m := range f.members {
		f.members[i].Online = s.presence.Online(m.User.ID)
		if m.User.ID != userID {
			continue
		}
		found = true
		snap.Permissions, _ = resolve(f.guild, f.roles, m)
		for _, c := range channels {
			if p := permission.Compute(snap.Permissions, guildID, userID, m.RoleIDs, c.Overwrites); p.Has(permission.ViewChannel) {
				snap.Channels = append(snap.Channels, SnapshotChannel{c, p})
			}
		}
	}
	if !found {
		return Snapshot{}, errs.ErrNotFound
	}
	return snap, nil
}

// Viewers returns the IDs of the members who can see a channel.
// shortcut: reloads roles and members on every call (once per message); cache per guild, invalidated on Sync, when it shows up in profiles.
func (s *Service) Viewers(ctx context.Context, channelID string) (map[string]bool, error) {
	c, err := s.store.Channel(ctx, channelID)
	if err != nil {
		return nil, err
	}
	f, err := s.load(ctx, c.GuildID)
	if err != nil {
		return nil, err
	}
	viewers := make(map[string]bool, len(f.members))
	for _, m := range f.members {
		base, _ := resolve(f.guild, f.roles, m)
		if permission.Compute(base, c.GuildID, m.User.ID, m.RoleIDs, c.Overwrites).Has(permission.ViewChannel) {
			viewers[m.User.ID] = true
		}
	}
	return viewers, nil
}
