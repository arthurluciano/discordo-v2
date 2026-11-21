package guild

import (
	"context"
	"slices"

	"discordo/server/internal/errs"
	"discordo/server/internal/id"
	"discordo/server/internal/permission"
)

type RoleInput struct {
	Name        *string         `json:"name"`
	Color       *int            `json:"color"`
	Permissions *permission.Set `json:"permissions"`
	Position    *int            `json:"position"`
}

func (s *Service) CreateRole(ctx context.Context, userID, guildID string, in RoleInput) (Role, error) {
	a, err := s.require(ctx, guildID, userID, permission.ManageRoles)
	if err != nil {
		return Role{}, err
	}
	return s.saveRole(ctx, a, Role{ID: id.New(), GuildID: guildID, Name: "novo cargo", Position: 1}, in)
}

func (s *Service) UpdateRole(ctx context.Context, userID, guildID, roleID string, in RoleInput) (Role, error) {
	a, r, err := s.manageableRole(ctx, userID, guildID, roleID)
	if err != nil {
		return Role{}, err
	}
	return s.saveRole(ctx, a, r, in)
}

// MoveRole swaps a role with the one right above or below it. Positions are renumbered 1..n in the current
// order first, so roles that tie (new ones all start at 1) still move one step.
func (s *Service) MoveRole(ctx context.Context, userID, guildID, roleID string, up bool) error {
	a, _, err := s.manageableRole(ctx, userID, guildID, roleID)
	if err != nil {
		return err
	}
	// a.roles is highest first (position desc, id); @everyone stays at 0.
	roles := slices.DeleteFunc(slices.Clone(a.roles), func(r Role) bool { return r.ID == guildID })
	i := slices.IndexFunc(roles, func(r Role) bool { return r.ID == roleID })
	j := i + 1
	if up {
		j = i - 1
	}
	if j < 0 || j >= len(roles) {
		return errs.Invalid("the role is already at the edge")
	}
	if roles[j].Position >= a.top {
		return errs.ErrForbidden
	}
	roles[i], roles[j] = roles[j], roles[i]
	for k := range roles {
		roles[k].Position = len(roles) - k
	}
	if err := s.store.SaveRoles(ctx, roles); err != nil {
		return err
	}
	s.sync(guildID)
	return nil
}

func (s *Service) DeleteRole(ctx context.Context, userID, guildID, roleID string) error {
	_, r, err := s.manageableRole(ctx, userID, guildID, roleID)
	if err != nil {
		return err
	}
	if r.ID == guildID {
		return errs.Invalid("@everyone cannot be deleted")
	}
	if err := s.store.DeleteRole(ctx, roleID); err != nil {
		return err
	}
	s.sync(guildID)
	return nil
}

func (s *Service) SetMemberRole(ctx context.Context, userID, guildID, targetID, roleID string, on bool) error {
	_, r, err := s.manageableRole(ctx, userID, guildID, roleID)
	if err != nil {
		return err
	}
	if r.ID == guildID {
		return errs.Invalid("@everyone cannot be assigned")
	}
	if err := s.store.SetMemberRole(ctx, guildID, targetID, roleID, on); err != nil {
		return err
	}
	s.sync(guildID)
	return nil
}

// manageableRole returns a role the actor is allowed to touch: they need ManageRoles and must outrank it.
func (s *Service) manageableRole(ctx context.Context, userID, guildID, roleID string) (actor, Role, error) {
	a, err := s.require(ctx, guildID, userID, permission.ManageRoles)
	if err != nil {
		return a, Role{}, err
	}
	i := slices.IndexFunc(a.roles, func(r Role) bool { return r.ID == roleID })
	if i < 0 {
		return a, Role{}, errs.ErrNotFound
	}
	if a.roles[i].Position >= a.top {
		return a, Role{}, errs.ErrForbidden
	}
	return a, a.roles[i], nil
}

func (s *Service) saveRole(ctx context.Context, a actor, r Role, in RoleInput) (Role, error) {
	everyone := r.ID == r.GuildID
	var err error
	if in.Name != nil && !everyone {
		if r.Name, err = cleanName(*in.Name); err != nil {
			return r, err
		}
	}
	if in.Color != nil {
		if *in.Color < 0 || *in.Color > 0xFFFFFF {
			return r, errs.Invalid("color must be an RGB value")
		}
		r.Color = *in.Color
	}
	if in.Permissions != nil {
		if *in.Permissions&^permission.All != 0 {
			return r, errs.Invalid("unknown permission bits")
		}
		// Nobody can grant or revoke a permission they do not hold themselves.
		if (*in.Permissions^r.Permissions)&^a.perms != 0 {
			return r, errs.ErrForbidden
		}
		r.Permissions = *in.Permissions
	}
	if in.Position != nil && !everyone {
		if *in.Position < 1 {
			return r, errs.Invalid("position must be at least 1")
		}
		r.Position = *in.Position
	}
	if r.Position >= a.top {
		return r, errs.ErrForbidden
	}
	if err := s.store.SaveRole(ctx, r); err != nil {
		return r, err
	}
	s.sync(r.GuildID)
	return r, nil
}
