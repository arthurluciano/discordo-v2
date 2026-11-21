// Package guild is the server ("guild") aggregate: the guild itself, its roles, members,
// channels and invites, and the permission rules that tie them together.
package guild

import (
	"context"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"discordo/server/internal/errs"
	"discordo/server/internal/event"
	"discordo/server/internal/id"
	"discordo/server/internal/image"
	"discordo/server/internal/permission"
	"discordo/server/internal/user"
)

type Guild struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IconURL   string `json:"icon_url"`
	BannerURL string `json:"banner_url"`
	OwnerID   string `json:"owner_id"`
}

// Role with ID == GuildID is @everyone: every member has it implicitly.
type Role struct {
	ID          string         `json:"id"`
	GuildID     string         `json:"guild_id"`
	Name        string         `json:"name"`
	Color       int            `json:"color"`
	Permissions permission.Set `json:"permissions"`
	Position    int            `json:"position"` // higher outranks lower
}

type Member struct {
	User     user.User `json:"user"`
	Nickname string    `json:"nickname"`
	RoleIDs  []string  `json:"roles"`
	JoinedAt time.Time `json:"joined_at"`
	Online   bool      `json:"online"`
}

type ChannelType int

const (
	Text ChannelType = iota
	Voice
	World ChannelType = 4 // the guild's map, one per guild: a call where everyone has a position. 2 and 3 are DMs
)

type Channel struct {
	ID         string                 `json:"id"`
	GuildID    string                 `json:"guild_id"`
	Type       ChannelType            `json:"type"`
	Name       string                 `json:"name"`
	Topic      string                 `json:"topic"`
	Position   int                    `json:"position"`
	Overwrites []permission.Overwrite `json:"overwrites"`
}

type Invite struct {
	Code      string     `json:"code"`
	GuildID   string     `json:"guild_id"`
	CreatorID string     `json:"creator_id"`
	MaxUses   int        `json:"max_uses"` // 0 = unlimited
	Uses      int        `json:"uses"`
	ExpiresAt *time.Time `json:"expires_at"` // nil = never
	Creator   *user.User `json:"creator,omitempty"`
}

type Ban struct {
	User user.User `json:"user"`
}

type Store interface {
	CreateGuild(ctx context.Context, g Guild, everyone Role, channels []Channel) error
	Guild(ctx context.Context, guildID string) (Guild, error)
	UpdateGuild(ctx context.Context, g Guild) error
	DeleteGuild(ctx context.Context, guildID string) error
	UserGuildIDs(ctx context.Context, userID string) ([]string, error)

	Roles(ctx context.Context, guildID string) ([]Role, error)
	SaveRole(ctx context.Context, r Role) error
	SaveRoles(ctx context.Context, roles []Role) error // in one transaction
	DeleteRole(ctx context.Context, roleID string) error

	Members(ctx context.Context, guildID string) ([]Member, error)
	Member(ctx context.Context, guildID, userID string) (Member, error)
	RemoveMember(ctx context.Context, guildID, userID string) error
	SetMemberRole(ctx context.Context, guildID, userID, roleID string, on bool) error
	// Ban records the ban and removes the membership, reporting whether there was one.
	Ban(ctx context.Context, guildID, userID string) (wasMember bool, err error)
	Unban(ctx context.Context, guildID, userID string) error
	Bans(ctx context.Context, guildID string) ([]Ban, error)

	Channels(ctx context.Context, guildID string) ([]Channel, error)
	Channel(ctx context.Context, channelID string) (Channel, error)
	SaveChannel(ctx context.Context, c Channel) error
	DeleteChannel(ctx context.Context, channelID string) error
	SetOverwrite(ctx context.Context, channelID string, o permission.Overwrite) error
	DeleteOverwrite(ctx context.Context, channelID, targetID string) error

	CreateInvite(ctx context.Context, inv Invite) error
	// Invites lists the guild's invites that have not expired by now, with their creators.
	Invites(ctx context.Context, guildID string, now time.Time) ([]Invite, error)
	Invite(ctx context.Context, code string) (Invite, error)
	DeleteInvite(ctx context.Context, code string) error
	// UseInvite adds the user to the invite's guild. ErrNotFound if the invite is unusable,
	// ErrForbidden if the user is banned. joined is false if they were already a member.
	UseInvite(ctx context.Context, code, userID string, now time.Time) (guildID string, joined bool, err error)
}

type Presence interface {
	Online(userID string) bool
}

type Service struct {
	// OnRemoved, when set, is called after a member leaves or is removed from a guild.
	OnRemoved func(guildID, userID string)

	store    Store
	pub      event.Publisher
	presence Presence
}

func NewService(store Store, pub event.Publisher, presence Presence) *Service {
	return &Service{store: store, pub: pub, presence: presence}
}

// actor is the requesting member with their guild-level permissions resolved.
type actor struct {
	guild  Guild
	member Member
	roles  []Role // all roles of the guild
	perms  permission.Set
	top    int // position of their highest role
}

// resolve returns a member's guild-level permissions and highest role position.
func resolve(g Guild, roles []Role, m Member) (permission.Set, int) {
	if g.OwnerID == m.User.ID {
		return permission.All, math.MaxInt
	}
	var perms permission.Set
	top := 0
	for _, r := range roles {
		if r.ID == g.ID || slices.Contains(m.RoleIDs, r.ID) {
			perms |= r.Permissions
			top = max(top, r.Position)
		}
	}
	if perms.Has(permission.Administrator) {
		perms = permission.All
	}
	return perms, top
}

// actor fails with ErrNotFound for non-members, so outsiders cannot probe which guilds exist.
func (s *Service) actor(ctx context.Context, guildID, userID string) (actor, error) {
	g, err := s.store.Guild(ctx, guildID)
	if err != nil {
		return actor{}, err
	}
	m, err := s.store.Member(ctx, guildID, userID)
	if err != nil {
		return actor{}, err
	}
	roles, err := s.store.Roles(ctx, guildID)
	if err != nil {
		return actor{}, err
	}
	perms, top := resolve(g, roles, m)
	return actor{g, m, roles, perms, top}, nil
}

func (s *Service) require(ctx context.Context, guildID, userID string, need permission.Set) (actor, error) {
	a, err := s.actor(ctx, guildID, userID)
	if err == nil && !a.perms.Has(need) {
		err = errs.ErrForbidden
	}
	return a, err
}

func (s *Service) sync(guildID string) {
	s.pub.Publish(event.Event{Kind: event.Sync, GuildID: guildID})
}

func (s *Service) Create(ctx context.Context, userID, name string) (Snapshot, error) {
	name, err := cleanName(name)
	if err != nil {
		return Snapshot{}, err
	}
	g := Guild{ID: id.New(), Name: name, OwnerID: userID}
	everyone := Role{ID: g.ID, GuildID: g.ID, Name: "@everyone", Permissions: permission.Default}
	channels := []Channel{
		{ID: id.New(), GuildID: g.ID, Type: Text, Name: "geral"},
		{ID: id.New(), GuildID: g.ID, Type: Voice, Name: "Geral", Position: 1},
		{ID: id.New(), GuildID: g.ID, Type: World, Name: "Mundo", Position: 2},
	}
	if err := s.store.CreateGuild(ctx, g, everyone, channels); err != nil {
		return Snapshot{}, err
	}
	s.pub.Publish(event.Event{Kind: event.Joined, GuildID: g.ID, UserIDs: []string{userID}})
	return s.snapshot(ctx, userID, g.ID)
}

type GuildInput struct {
	Name      *string `json:"name"`
	IconURL   *string `json:"icon_url"`
	BannerURL *string `json:"banner_url"`
}

func (s *Service) Update(ctx context.Context, userID, guildID string, in GuildInput) error {
	a, err := s.require(ctx, guildID, userID, permission.ManageGuild)
	if err != nil {
		return err
	}
	g := a.guild
	if in.Name != nil {
		if g.Name, err = cleanName(*in.Name); err != nil {
			return err
		}
	}
	for _, f := range []struct{ in, out *string }{{in.IconURL, &g.IconURL}, {in.BannerURL, &g.BannerURL}} {
		if f.in != nil {
			if !image.Valid(*f.in) {
				return errs.Invalid("icon_url and banner_url must be empty or an uploaded image (POST /images)")
			}
			*f.out = *f.in
		}
	}
	if err := s.store.UpdateGuild(ctx, g); err != nil {
		return err
	}
	s.sync(guildID)
	return nil
}

func (s *Service) Delete(ctx context.Context, userID, guildID string) error {
	a, err := s.actor(ctx, guildID, userID)
	if err != nil {
		return err
	}
	if a.guild.OwnerID != userID {
		return errs.ErrForbidden
	}
	if err := s.store.DeleteGuild(ctx, guildID); err != nil {
		return err
	}
	s.pub.Publish(event.Event{Kind: event.ToGuild, GuildID: guildID, Type: "GUILD_DELETE", Data: map[string]string{"id": guildID}})
	return nil
}

func (s *Service) UserGuildIDs(ctx context.Context, userID string) ([]string, error) {
	return s.store.UserGuildIDs(ctx, userID)
}

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > 100 {
		return "", errs.Invalid("name must have 1 to 100 characters")
	}
	return name, nil
}
