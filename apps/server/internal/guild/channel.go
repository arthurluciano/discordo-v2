package guild

import (
	"context"
	"slices"
	"unicode/utf8"

	"discordo/server/internal/errs"
	"discordo/server/internal/id"
	"discordo/server/internal/permission"
)

type ChannelInput struct {
	Type     ChannelType `json:"type"` // only used on create
	Name     *string     `json:"name"`
	Topic    *string     `json:"topic"`
	Position *int        `json:"position"`
}

// ChannelAccess returns the user's permissions in a channel and the guild it belongs to.
func (s *Service) ChannelAccess(ctx context.Context, userID, channelID string) (permission.Set, string, error) {
	_, c, perms, err := s.channel(ctx, userID, channelID)
	return perms, c.GuildID, err
}

func (s *Service) channel(ctx context.Context, userID, channelID string) (actor, Channel, permission.Set, error) {
	c, err := s.store.Channel(ctx, channelID)
	if err != nil {
		return actor{}, c, 0, err
	}
	a, err := s.actor(ctx, c.GuildID, userID)
	if err != nil {
		return a, c, 0, err
	}
	perms := permission.Compute(a.perms, c.GuildID, userID, a.member.RoleIDs, c.Overwrites)
	if !perms.Has(permission.ViewChannel) {
		return a, c, 0, errs.ErrNotFound // hidden channels look nonexistent
	}
	return a, c, perms, nil
}

func (s *Service) channelWith(ctx context.Context, userID, channelID string, need permission.Set) (actor, Channel, permission.Set, error) {
	a, c, perms, err := s.channel(ctx, userID, channelID)
	if err == nil && !perms.Has(need) {
		err = errs.ErrForbidden
	}
	return a, c, perms, err
}

func (s *Service) CreateChannel(ctx context.Context, userID, guildID string, in ChannelInput) (Channel, error) {
	if _, err := s.require(ctx, guildID, userID, permission.ManageChannels); err != nil {
		return Channel{}, err
	}
	if in.Type != Text && in.Type != Voice {
		return Channel{}, errs.Invalid("unknown channel type")
	}
	if in.Name == nil {
		return Channel{}, errs.Invalid("name is required")
	}
	return s.saveChannel(ctx, Channel{ID: id.New(), GuildID: guildID, Type: in.Type, Overwrites: []permission.Overwrite{}}, in)
}

func (s *Service) UpdateChannel(ctx context.Context, userID, channelID string, in ChannelInput) (Channel, error) {
	_, c, _, err := s.channelWith(ctx, userID, channelID, permission.ManageChannels)
	if err != nil {
		return c, err
	}
	return s.saveChannel(ctx, c, in)
}

func (s *Service) saveChannel(ctx context.Context, c Channel, in ChannelInput) (Channel, error) {
	var err error
	if in.Name != nil {
		if c.Name, err = cleanName(*in.Name); err != nil {
			return c, err
		}
	}
	if in.Topic != nil {
		if utf8.RuneCountInString(*in.Topic) > 1024 {
			return c, errs.Invalid("topic must have at most 1024 characters")
		}
		c.Topic = *in.Topic
	}
	if in.Position != nil {
		c.Position = *in.Position
	}
	if err := s.store.SaveChannel(ctx, c); err != nil {
		return c, err
	}
	s.sync(c.GuildID)
	return c, nil
}

func (s *Service) DeleteChannel(ctx context.Context, userID, channelID string) error {
	_, c, _, err := s.channelWith(ctx, userID, channelID, permission.ManageChannels)
	if err != nil {
		return err
	}
	if err := s.store.DeleteChannel(ctx, channelID); err != nil {
		return err
	}
	s.sync(c.GuildID)
	return nil
}

func (s *Service) SetOverwrite(ctx context.Context, userID, channelID string, o permission.Overwrite) error {
	a, c, perms, err := s.channelWith(ctx, userID, channelID, permission.ManageRoles)
	if err != nil {
		return err
	}
	touched := o.Allow | o.Deny
	if touched&^permission.ChannelScoped != 0 || o.Allow&o.Deny != 0 {
		return errs.Invalid("overwrite has bits that are not channel permissions, or allows and denies the same bit")
	}
	if touched&^perms != 0 {
		return errs.ErrForbidden // cannot hand out or take away what you do not have here
	}
	if o.IsMember {
		if _, err := s.store.Member(ctx, c.GuildID, o.TargetID); err != nil {
			return err
		}
	} else if !slices.ContainsFunc(a.roles, func(r Role) bool { return r.ID == o.TargetID }) {
		return errs.ErrNotFound
	}
	if err := s.store.SetOverwrite(ctx, channelID, o); err != nil {
		return err
	}
	s.sync(c.GuildID)
	return nil
}

func (s *Service) DeleteOverwrite(ctx context.Context, userID, channelID, targetID string) error {
	_, c, _, err := s.channelWith(ctx, userID, channelID, permission.ManageRoles)
	if err != nil {
		return err
	}
	if err := s.store.DeleteOverwrite(ctx, channelID, targetID); err != nil {
		return err
	}
	s.sync(c.GuildID)
	return nil
}
