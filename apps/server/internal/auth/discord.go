package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
)

type Discord struct{ cfg oauth2.Config }

func NewDiscord(clientID, clientSecret, redirectURL string) *Discord {
	return &Discord{oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{"identify"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://discord.com/oauth2/authorize",
			TokenURL: "https://discord.com/api/oauth2/token",
		},
	}}
}

func (d *Discord) AuthURL(state, verifier string) string {
	return d.cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
}

func (d *Discord) Identity(ctx context.Context, code, verifier string) (Identity, error) {
	tok, err := d.cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Identity{}, err
	}
	resp, err := d.cfg.Client(ctx, tok).Get("https://discord.com/api/users/@me")
	if err != nil {
		return Identity{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Identity{}, fmt.Errorf("discord /users/@me: %s", resp.Status)
	}
	var u struct {
		ID         string `json:"id"`
		Username   string `json:"username"`
		GlobalName string `json:"global_name"`
		Avatar     string `json:"avatar"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return Identity{}, err
	}
	idn := Identity{ProviderID: u.ID, Username: u.Username, DisplayName: u.GlobalName}
	if idn.DisplayName == "" {
		idn.DisplayName = u.Username
	}
	if u.Avatar != "" {
		idn.AvatarURL = fmt.Sprintf("https://cdn.discordapp.com/avatars/%s/%s.png", u.ID, u.Avatar)
	}
	return idn, nil
}
