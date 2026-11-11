package guild

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"discordo/server/internal/errs"
	"discordo/server/internal/event"
	"discordo/server/internal/permission"
)

const maxInviteTTL = 30 * 24 * time.Hour

func (s *Service) Leave(ctx context.Context, userID, guildID string) error {
	a, err := s.actor(ctx, guildID, userID)
	if err != nil {
		return err
	}
	if a.guild.OwnerID == userID {
		return errs.Invalid("the owner cannot leave; delete the guild instead")
	}
	return s.remove(ctx, guildID, userID)
}

func (s *Service) Kick(ctx context.Context, userID, guildID, targetID string) error {
	a, err := s.require(ctx, guildID, userID, permission.KickMembers)
	if err != nil {
		return err
	}
	if err := s.outranks(ctx, a, targetID); err != nil {
		return err
	}
	return s.remove(ctx, guildID, targetID)
}

// Ban also works on users who are not members, to keep them from joining.
func (s *Service) Ban(ctx context.Context, userID, guildID, targetID string) error {
	a, err := s.require(ctx, guildID, userID, permission.BanMembers)
	if err != nil {
		return err
	}
	if err := s.outranks(ctx, a, targetID); err != nil && !errors.Is(err, errs.ErrNotFound) {
		return err
	}
	wasMember, err := s.store.Ban(ctx, guildID, targetID)
	if err != nil {
		return err
	}
	if wasMember {
		s.removed(guildID, targetID)
	}
	return nil
}

func (s *Service) Unban(ctx context.Context, userID, guildID, targetID string) error {
	if _, err := s.require(ctx, guildID, userID, permission.BanMembers); err != nil {
		return err
	}
	return s.store.Unban(ctx, guildID, targetID)
}

// outranks fails unless the actor may moderate the target. ErrNotFound if the target is not a member.
func (s *Service) outranks(ctx context.Context, a actor, targetID string) error {
	if targetID == a.guild.OwnerID {
		return errs.ErrForbidden
	}
	m, err := s.store.Member(ctx, a.guild.ID, targetID)
	if err != nil {
		return err
	}
	if _, top := resolve(a.guild, a.roles, m); top >= a.top {
		return errs.ErrForbidden
	}
	return nil
}

func (s *Service) remove(ctx context.Context, guildID, userID string) error {
	if err := s.store.RemoveMember(ctx, guildID, userID); err != nil {
		return err
	}
	s.removed(guildID, userID)
	return nil
}

func (s *Service) removed(guildID, userID string) {
	if s.OnRemoved != nil {
		s.OnRemoved(guildID, userID)
	}
	s.pub.Publish(event.Event{Kind: event.Left, GuildID: guildID, UserIDs: []string{userID}})
	s.pub.Publish(event.Event{Kind: event.ToGuild, GuildID: guildID, Type: "MEMBER_REMOVE",
		Data: map[string]string{"guild_id": guildID, "user_id": userID}})
}

func (s *Service) CreateInvite(ctx context.Context, userID, guildID string, maxUses int, ttl time.Duration) (Invite, error) {
	if _, err := s.require(ctx, guildID, userID, permission.CreateInvite); err != nil {
		return Invite{}, err
	}
	if maxUses < 0 || maxUses > 1000 || ttl < 0 || ttl > maxInviteTTL {
		return Invite{}, errs.Invalid("max_uses must be 0 to 1000 and max_age at most 30 days")
	}
	inv := Invite{Code: rand.Text()[:10], GuildID: guildID, CreatorID: userID, MaxUses: maxUses}
	if ttl > 0 {
		t := time.Now().Add(ttl)
		inv.ExpiresAt = &t
	}
	return inv, s.store.CreateInvite(ctx, inv)
}

func (s *Service) Join(ctx context.Context, userID, code string) (Snapshot, error) {
	guildID, joined, err := s.store.UseInvite(ctx, code, userID, time.Now())
	if err != nil {
		return Snapshot{}, err
	}
	if joined {
		m, err := s.store.Member(ctx, guildID, userID)
		if err != nil {
			return Snapshot{}, err
		}
		m.Online = s.presence.Online(userID)
		s.pub.Publish(event.Event{Kind: event.ToGuild, GuildID: guildID, Type: "MEMBER_ADD",
			Data: map[string]any{"guild_id": guildID, "member": m}})
		s.pub.Publish(event.Event{Kind: event.Joined, GuildID: guildID, UserIDs: []string{userID}})
	}
	return s.snapshot(ctx, userID, guildID)
}
