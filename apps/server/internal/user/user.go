package user

import "time"

// User is the public profile; safe to send to any other user.
type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
}

// Details are the rest of a profile: fetched when a profile card opens, not sent with every member and message.
type Details struct {
	Bio      string `json:"bio"`
	Pronouns string `json:"pronouns"`
	// 0xRRGGBB, or 0 for the theme's own banner.
	BannerColor int       `json:"banner_color"`
	CreatedAt   time.Time `json:"created_at"`
}

// Profile is what a profile card shows (GET /users/{user}/profile).
type Profile struct {
	User
	Details
	// The servers the viewer shares with this user (empty on their own profile).
	MutualGuildIDs []string `json:"mutual_guild_ids"`
}
