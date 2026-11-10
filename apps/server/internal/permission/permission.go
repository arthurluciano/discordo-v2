// Package permission is the pure permission math: bitfields and channel overwrites.
package permission

import "slices"

type Set int64

const (
	ViewChannel Set = 1 << iota
	SendMessages
	ManageMessages
	AttachFiles
	AddReactions
	MentionEveryone
	Connect
	Speak
	Stream
	CreateInvite
	KickMembers
	BanMembers
	ManageChannels
	ManageRoles
	ManageGuild
	Administrator
)

const (
	All = Administrator<<1 - 1
	// Default is what @everyone gets in a new guild.
	Default = ViewChannel | SendMessages | AttachFiles | AddReactions | Connect | Speak | Stream | CreateInvite
	// ChannelScoped are the bits a channel overwrite may touch.
	ChannelScoped = ViewChannel | SendMessages | ManageMessages | AttachFiles | AddReactions |
		MentionEveryone | Connect | Speak | Stream | ManageChannels | ManageRoles
)

func (s Set) Has(p Set) bool { return s&p == p }

type Overwrite struct {
	TargetID string `json:"target_id"` // role ID or user ID
	IsMember bool   `json:"is_member"`
	Allow    Set    `json:"allow"`
	Deny     Set    `json:"deny"`
}

// Compute applies channel overwrites to a member's guild-level permissions.
// Order: @everyone overwrite, then the member's roles (allow wins over deny), then the member.
func Compute(base Set, everyoneID, userID string, roleIDs []string, overwrites []Overwrite) Set {
	if base.Has(Administrator) {
		return All
	}
	p := base
	var roleAllow, roleDeny, memberAllow, memberDeny Set
	for _, o := range overwrites {
		switch {
		case o.IsMember:
			if o.TargetID == userID {
				memberAllow, memberDeny = o.Allow, o.Deny
			}
		case o.TargetID == everyoneID:
			p = p&^o.Deny | o.Allow
		case slices.Contains(roleIDs, o.TargetID):
			roleAllow |= o.Allow
			roleDeny |= o.Deny
		}
	}
	p = p&^roleDeny | roleAllow
	return p&^memberDeny | memberAllow
}
