package app_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/jackc/pgx/v5"

	"discordo/server/internal/app"
	"discordo/server/internal/auth"
	"discordo/server/internal/blob"
	"discordo/server/internal/media/pion"
	"discordo/server/internal/permission"
	"discordo/server/internal/postgres"
)

// fakeProvider treats the OAuth code as the user's name.
type fakeProvider struct{}

func (fakeProvider) AuthURL(state, _ string) string { return "http://provider.test/?state=" + state }
func (fakeProvider) Identity(_ context.Context, code, _ string) (auth.Identity, error) {
	return auth.Identity{ProviderID: code, Username: code, DisplayName: code}, nil
}

type env struct {
	t   *testing.T
	url string
}

// start boots the whole server against TEST_DATABASE_URL, wiping it first.
func start(t *testing.T) env {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL and TEST_S3_ENDPOINT not set (run `make test`)")
	}
	u, err := url.Parse(dbURL)
	if err != nil || !strings.HasSuffix(u.Path, "_test") {
		t.Fatal("TEST_DATABASE_URL must point at a database whose name ends in _test: it gets wiped")
	}
	ctx := t.Context()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `drop schema public cascade; create schema public`); err != nil {
		t.Fatal(err)
	}
	conn.Close(ctx)

	store, err := postgres.Open(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	blobs, err := blob.NewS3(ctx, blob.Config{
		Endpoint: os.Getenv("TEST_S3_ENDPOINT"), AccessKey: "discordo", SecretKey: "discordo-secret", Bucket: "discordo-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	sfu, err := pion.New(pion.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sfu.Close() })
	handler, hub := app.New(store, fakeProvider{}, app.Options{PublicURL: "http://localhost", Blobs: blobs, Media: sfu})
	go hub.Run(ctx)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return env{t, srv.URL}
}

// call makes a JSON request and returns the status, decoding the body into out when given.
func (e env) call(token, method, path string, body, out any) int {
	e.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, e.url+path, &buf)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			e.t.Fatalf("%s %s: decoding %d response: %v", method, path, resp.StatusCode, err)
		}
	}
	return resp.StatusCode
}

func (e env) must(token, method, path string, body, out any) {
	e.t.Helper()
	if status := e.call(token, method, path, body, out); status >= 300 {
		e.t.Fatalf("%s %s: status %d", method, path, status)
	}
}

// login walks the browser flow and returns the session token (the cookie's value) and the user's ID.
func (e env) login(name string) (token, userID string) {
	e.t.Helper()
	browser := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := browser.Get(e.url + "/auth/discord/login")
	if err != nil || resp.StatusCode != http.StatusFound {
		e.t.Fatalf("login: %v %v", err, resp)
	}
	toProvider, _ := url.Parse(resp.Header.Get("Location"))

	req, _ := http.NewRequest("GET", e.url+"/auth/discord/callback?code="+name+"&state="+toProvider.Query().Get("state"), nil)
	for _, c := range resp.Cookies() {
		req.AddCookie(c)
	}
	resp, err = browser.Do(req)
	if err != nil || resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		e.t.Fatalf("callback: %v %v", err, resp)
	}
	for _, c := range resp.Cookies() {
		if c.Name == auth.SessionCookie && c.HttpOnly {
			token = c.Value
		}
	}
	var me struct{ ID string }
	e.must(token, "GET", "/users/@me", nil, &me)
	return token, me.ID
}

// browserCall makes a request the way the web app does: session cookie, optional Origin, no Authorization.
func (e env) browserCall(token, origin, method, path string, body io.Reader, contentType string) *http.Response {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.url+path, body)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: token})
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

type frame struct {
	T string
	D json.RawMessage
}

type socket struct {
	t *testing.T
	c *websocket.Conn
}

func (e env) connect(token string) socket {
	e.t.Helper()
	c, _, err := websocket.Dial(e.t.Context(), "ws"+strings.TrimPrefix(e.url, "http")+"/gateway", nil)
	if err != nil {
		e.t.Fatal(err)
	}
	c.SetReadLimit(1 << 20)
	e.t.Cleanup(func() { c.CloseNow() })
	if err := wsjson.Write(e.t.Context(), c, map[string]string{"token": token}); err != nil {
		e.t.Fatal(err)
	}
	return socket{e.t, c}
}

// next skips frames until one of the given type and decodes its data into out.
func (s socket) next(typ string, out any) {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(s.t.Context(), 5*time.Second)
	defer cancel()
	for {
		var f frame
		if err := wsjson.Read(ctx, s.c, &f); err != nil {
			s.t.Fatalf("waiting for %s: %v", typ, err)
		}
		if f.T == typ {
			if out != nil {
				if err := json.Unmarshal(f.D, out); err != nil {
					s.t.Fatal(err)
				}
			}
			return
		}
	}
}

type snapshot struct {
	ID       string
	Channels []struct {
		ID, Name    string
		Permissions permission.Set
	}
	Members []struct {
		User   struct{ ID string }
		Online bool
	}
}

func TestDesktopLogin(t *testing.T) {
	e := start(t)
	browser := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	// A port or nonce the app could not have sent is refused before the provider.
	for _, q := range []string{"port=80&nonce=0123456789abcdef", "port=5000&nonce=short", "port=x&nonce=0123456789abcdef"} {
		if resp, _ := browser.Get(e.url + "/auth/discord/login?" + q); resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status %d", q, resp.StatusCode)
		}
	}

	resp, _ := browser.Get(e.url + "/auth/discord/login?port=53682&nonce=0123456789abcdef")
	toProvider, _ := url.Parse(resp.Header.Get("Location"))
	req, _ := http.NewRequest("GET", e.url+"/auth/discord/callback?code=alice&state="+toProvider.Query().Get("state"), nil)
	for _, c := range resp.Cookies() {
		req.AddCookie(c)
	}

	resp, _ = browser.Do(req)
	back, _ := url.Parse(resp.Header.Get("Location"))
	if back.Host != "127.0.0.1:53682" || back.Path != "/callback" || back.Query().Get("nonce") != "0123456789abcdef" {
		t.Fatalf("redirect: %s", back)
	}

	for _, c := range resp.Cookies() {
		if c.Name == auth.SessionCookie {
			t.Fatal("the desktop login must not set the browser's session cookie")
		}
	}

	// The token in the redirect is a session like the web's.
	e.must(back.Query().Get("token"), "GET", "/users/@me", nil, nil)
}

func TestWebSession(t *testing.T) {
	e := start(t)
	browser := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	// A callback whose state is not the one in the flow cookie is refused.
	resp, _ := browser.Get(e.url + "/auth/discord/login")
	req, _ := http.NewRequest("GET", e.url+"/auth/discord/callback?code=mallory&state=forged", nil)
	for _, c := range resp.Cookies() {
		req.AddCookie(c)
	}
	if resp, _ = browser.Do(req); resp.Header.Get("Location") != "/?login_error=cancelled" || len(resp.Cookies()) != 1 {
		t.Fatalf("forged state: %v, cookies %v", resp.Header.Get("Location"), resp.Cookies()) // only the flow cookie, cleared
	}

	token, _ := e.login("alice")
	origin := e.url // httptest serves on the same host the requests go to
	if resp := e.browserCall(token, "", "GET", "/users/@me", nil, ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("cookie GET: status %d", resp.StatusCode)
	}
	// Writes with the cookie need our own Origin: another site cannot make the browser do them (CSRF).
	body := `{"name":"Oficina"}`
	if resp := e.browserCall(token, "https://evil.example", "POST", "/guilds", strings.NewReader(body), "application/json"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site POST: status %d", resp.StatusCode)
	}
	if resp := e.browserCall(token, "", "POST", "/guilds", strings.NewReader(body), "application/json"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST without Origin: status %d", resp.StatusCode)
	}
	if resp := e.browserCall(token, origin, "POST", "/guilds", strings.NewReader(body), "application/json"); resp.StatusCode != http.StatusOK {
		t.Fatalf("same-origin POST: status %d", resp.StatusCode)
	}

	// The gateway takes the cookie when the hello has no token.
	header := http.Header{}
	header.Add("Cookie", auth.SessionCookie+"="+token)
	c, _, err := websocket.Dial(t.Context(), "ws"+strings.TrimPrefix(e.url, "http")+"/gateway", &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	c.SetReadLimit(1 << 20)
	t.Cleanup(func() { c.CloseNow() })
	if err := wsjson.Write(t.Context(), c, map[string]string{}); err != nil {
		t.Fatal(err)
	}
	socket{t, c}.next("READY", nil)

	// Logging out ends the session and clears the cookie.
	resp = e.browserCall(token, origin, "POST", "/auth/logout", nil, "")
	if resp.StatusCode != http.StatusNoContent || len(resp.Cookies()) != 1 || resp.Cookies()[0].MaxAge >= 0 {
		t.Fatalf("logout: status %d, cookies %v", resp.StatusCode, resp.Cookies())
	}
	if status := e.call(token, "GET", "/users/@me", nil, nil); status != http.StatusUnauthorized {
		t.Fatalf("after logout: status %d", status)
	}
	if status := e.call("not-a-token", "GET", "/users/@me", nil, nil); status != http.StatusUnauthorized {
		t.Fatalf("bad token: status %d", status)
	}
}

// pngBytes is the smallest valid PNG: one transparent pixel.
var pngBytes, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=")

// uploadImage posts a picture to /images and returns its URL.
func (e env) uploadImage(token string, content []byte) (int, string) {
	e.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, _ := w.CreateFormFile("file", "picture")
	part.Write(content)
	w.Close()
	req, _ := http.NewRequest("POST", e.url+"/images", &buf)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct{ URL string }
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out.URL
}

func TestProfilesAndImages(t *testing.T) {
	e := start(t)
	alice, aliceID := e.login("alice")
	bob, _ := e.login("bob")
	var guild snapshot
	e.must(alice, "POST", "/guilds", map[string]string{"name": "Oficina"}, &guild)
	var invite struct{ Code string }
	e.must(alice, "POST", "/guilds/"+guild.ID+"/invites", map[string]int{}, &invite)
	e.must(bob, "POST", "/invites/"+invite.Code, nil, nil)
	bobWS := e.connect(bob)
	bobWS.next("READY", nil)

	if status, _ := e.uploadImage(alice, []byte("not an image at all")); status != http.StatusBadRequest {
		t.Fatalf("non-image upload: status %d", status)
	}
	status, picture := e.uploadImage(alice, pngBytes)
	if status != http.StatusOK || !strings.HasPrefix(picture, "/images/") {
		t.Fatalf("upload: status %d, url %q", status, picture)
	}
	// Images are public: <img> tags carry no credentials.
	resp, err := http.Get(e.url + picture)
	if err != nil || resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("GET image: %v %v", err, resp)
	}
	resp.Body.Close()

	// Avatars must be our own uploads; a third-party URL would make every viewer fetch from it.
	if status := e.call(alice, "PATCH", "/users/@me", map[string]string{"avatar_url": "https://tracker.example/a.png"}, nil); status != http.StatusBadRequest {
		t.Fatalf("outside avatar: status %d", status)
	}
	e.must(alice, "PATCH", "/users/@me", map[string]string{"avatar_url": picture, "display_name": "Alice A."}, nil)
	var updated struct {
		ID          string
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
	}
	if bobWS.next("USER_UPDATE", &updated); updated.ID != aliceID || updated.AvatarURL != picture || updated.DisplayName != "Alice A." {
		t.Fatalf("USER_UPDATE: %+v", updated)
	}
	// Logging in again does not bring back the name and avatar from Discord.
	again, _ := e.login("alice")
	var me struct {
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
	}
	if e.must(again, "GET", "/users/@me", nil, &me); me.AvatarURL != picture || me.DisplayName != "Alice A." {
		t.Fatalf("profile after login: %+v", me)
	}

	// Profile details: anyone who shares a server sees them, with the servers in common; strangers get not found.
	if status := e.call(alice, "PATCH", "/users/@me", map[string]string{"bio": strings.Repeat("a", 191)}, nil); status != http.StatusBadRequest {
		t.Fatalf("long bio: status %d", status)
	}
	if status := e.call(alice, "PATCH", "/users/@me", map[string]int{"banner_color": 0x1000000}, nil); status != http.StatusBadRequest {
		t.Fatalf("bad banner color: status %d", status)
	}
	e.must(alice, "PATCH", "/users/@me", map[string]any{"bio": " Faz bolos. ", "pronouns": "ela/dela", "banner_color": 0xff2a6d}, nil)
	var profile struct {
		ID             string
		DisplayName    string   `json:"display_name"`
		Bio            string   `json:"bio"`
		Pronouns       string   `json:"pronouns"`
		BannerColor    int      `json:"banner_color"`
		CreatedAt      string   `json:"created_at"`
		MutualGuildIDs []string `json:"mutual_guild_ids"`
	}
	e.must(bob, "GET", "/users/"+aliceID+"/profile", nil, &profile)
	if profile.ID != aliceID || profile.DisplayName != "Alice A." || profile.Bio != "Faz bolos." || profile.Pronouns != "ela/dela" ||
		profile.BannerColor != 0xff2a6d || profile.CreatedAt == "" || len(profile.MutualGuildIDs) != 1 || profile.MutualGuildIDs[0] != guild.ID {
		t.Fatalf("profile seen by bob: %+v", profile)
	}
	stranger, _ := e.login("carol")
	if status := e.call(stranger, "GET", "/users/"+aliceID+"/profile", nil, nil); status != http.StatusNotFound {
		t.Fatalf("stranger viewing a profile: status %d", status)
	}
	// Server icon and banner.
	e.must(alice, "PATCH", "/guilds/"+guild.ID, map[string]string{"banner_url": picture, "icon_url": picture}, nil)
	var synced struct {
		IconURL   string `json:"icon_url"`
		BannerURL string `json:"banner_url"`
	}
	if bobWS.next("GUILD_SYNC", &synced); synced.BannerURL != picture || synced.IconURL != picture {
		t.Fatalf("GUILD_SYNC: %+v", synced)
	}
	if status := e.call(bob, "PATCH", "/guilds/"+guild.ID, map[string]string{"banner_url": ""}, nil); status != http.StatusForbidden {
		t.Fatalf("banner without permission: status %d", status)
	}
}

func TestGuildRealtimeFlow(t *testing.T) {
	e := start(t)
	alice, _ := e.login("alice")
	bob, bobID := e.login("bob")

	// Alice creates a guild and invites Bob.
	var guild snapshot
	e.must(alice, "POST", "/guilds", map[string]string{"name": "Oficina"}, &guild)
	if len(guild.Channels) != 3 {
		t.Fatalf("new guild has %d channels, want 3", len(guild.Channels))
	}
	general := guild.Channels[0].ID
	var invite struct{ Code string }
	e.must(alice, "POST", "/guilds/"+guild.ID+"/invites", map[string]int{"max_uses": 1}, &invite)

	aliceWS := e.connect(alice)
	aliceWS.next("READY", nil)

	e.must(bob, "POST", "/invites/"+invite.Code, nil, nil)
	aliceWS.next("MEMBER_ADD", nil)
	if carol, _ := e.login("carol"); e.call(carol, "POST", "/invites/"+invite.Code, nil, nil) != http.StatusNotFound {
		t.Fatal("invite worked past max_uses")
	}

	bobWS := e.connect(bob)
	var ready struct{ Guilds []snapshot }
	bobWS.next("READY", &ready)
	if len(ready.Guilds) != 1 || len(ready.Guilds[0].Members) != 2 {
		t.Fatalf("bob's READY: %+v", ready)
	}
	var presence struct {
		UserID string `json:"user_id"`
		Online bool
	}
	aliceWS.next("PRESENCE_UPDATE", &presence)
	if presence.UserID != bobID || !presence.Online {
		t.Fatalf("presence: %+v", presence)
	}

	// A message reaches the other member in real time.
	var msg struct {
		Content string
		Author  struct{ Username string }
	}
	e.must(alice, "POST", "/channels/"+general+"/messages", map[string]string{"content": "  olá  "}, nil)
	bobWS.next("MESSAGE_CREATE", &msg)
	if msg.Content != "olá" || msg.Author.Username != "alice" {
		t.Fatalf("message: %+v", msg)
	}

	// Alice makes a channel only she can see.
	var secret struct{ ID string }
	e.must(alice, "POST", "/guilds/"+guild.ID+"/channels", map[string]any{"name": "secreto"}, &secret)
	e.must(alice, "PUT", "/channels/"+secret.ID+"/overwrites/"+guild.ID, permission.Overwrite{Deny: permission.ViewChannel}, nil)
	var synced snapshot
	bobWS.next("GUILD_SYNC", &synced) // after the channel was created: Bob sees it
	bobWS.next("GUILD_SYNC", &synced) // after the overwrite: he does not
	for _, c := range synced.Channels {
		if c.ID == secret.ID {
			t.Fatal("hidden channel is in bob's snapshot")
		}
		if !c.Permissions.Has(permission.ViewChannel | permission.SendMessages) {
			t.Fatalf("bob's permissions in %s: %b", c.Name, c.Permissions)
		}
	}

	// Bob can neither read, write, nor receive events of the hidden channel.
	if status := e.call(bob, "POST", "/channels/"+secret.ID+"/messages", map[string]string{"content": "oi"}, nil); status != http.StatusNotFound {
		t.Fatalf("bob posting to hidden channel: status %d", status)
	}
	if status := e.call(bob, "GET", "/channels/"+secret.ID+"/messages", nil, nil); status != http.StatusNotFound {
		t.Fatalf("bob reading hidden channel: status %d", status)
	}
	e.must(alice, "POST", "/channels/"+secret.ID+"/messages", map[string]string{"content": "segredo"}, nil)
	e.must(alice, "POST", "/channels/"+general+"/messages", map[string]string{"content": "público"}, nil)
	bobWS.next("MESSAGE_CREATE", &msg)
	if msg.Content != "público" {
		t.Fatalf("bob received %q from a channel he cannot see", msg.Content)
	}

	// History is paginated newest first.
	var history []struct{ ID, Content string }
	e.must(bob, "GET", "/channels/"+general+"/messages?limit=1", nil, &history)
	if len(history) != 1 || history[0].Content != "público" {
		t.Fatalf("history: %+v", history)
	}
	e.must(bob, "GET", "/channels/"+general+"/messages?before="+history[0].ID, nil, &history)
	if len(history) != 1 || history[0].Content != "olá" {
		t.Fatalf("history before cursor: %+v", history)
	}

	// Permissions: Bob cannot manage; a role from Alice changes that, within limits.
	if status := e.call(bob, "POST", "/guilds/"+guild.ID+"/channels", map[string]any{"name": "x"}, nil); status != http.StatusForbidden {
		t.Fatalf("bob creating a channel: status %d", status)
	}
	if status := e.call(bob, "DELETE", "/guilds/"+guild.ID, nil, nil); status != http.StatusForbidden {
		t.Fatalf("bob deleting the guild: status %d", status)
	}
	var mod struct{ ID string }
	e.must(alice, "POST", "/guilds/"+guild.ID+"/roles", map[string]any{
		"name": "mod", "position": 5, "permissions": permission.Default | permission.ManageChannels | permission.ManageRoles | permission.KickMembers,
	}, &mod)
	e.must(alice, "PUT", "/guilds/"+guild.ID+"/members/"+bobID+"/roles/"+mod.ID, nil, nil)
	e.must(bob, "POST", "/guilds/"+guild.ID+"/channels", map[string]any{"name": "do-bob", "type": 1}, nil)
	if status := e.call(bob, "POST", "/guilds/"+guild.ID+"/roles", map[string]any{"permissions": permission.Administrator}, nil); status != http.StatusForbidden {
		t.Fatalf("bob granting a permission he lacks: status %d", status)
	}
	if status := e.call(bob, "PATCH", "/guilds/"+guild.ID+"/roles/"+mod.ID, map[string]any{"name": "dono"}, nil); status != http.StatusForbidden {
		t.Fatalf("bob editing his own top role: status %d", status)
	}
	if status := e.call(bob, "DELETE", "/guilds/"+guild.ID+"/members/"+guild.Members[0].User.ID, nil, nil); status != http.StatusForbidden {
		t.Fatalf("bob kicking the owner: status %d", status)
	}

	// Moving a role swaps it with its neighbor, even when new roles tie at position 1.
	var low, high struct{ ID string }
	e.must(alice, "POST", "/guilds/"+guild.ID+"/roles", map[string]any{"name": "a"}, &low)
	e.must(alice, "POST", "/guilds/"+guild.ID+"/roles", map[string]any{"name": "b"}, &high)
	e.must(alice, "POST", "/guilds/"+guild.ID+"/roles/"+high.ID+"/move", map[string]bool{"up": true}, nil)
	if status := e.call(alice, "POST", "/guilds/"+guild.ID+"/roles/"+low.ID+"/move", map[string]bool{"up": false}, nil); status != http.StatusBadRequest {
		t.Fatalf("moving the lowest role down: status %d", status)
	}
	if status := e.call(bob, "POST", "/guilds/"+guild.ID+"/roles/"+high.ID+"/move", map[string]bool{"up": true}, nil); status != http.StatusForbidden {
		t.Fatalf("bob moving a role above his own: status %d", status)
	}

	// Invites: only ManageGuild lists them; the creator or ManageGuild revokes.
	var bobInvite, aliceInvite struct{ Code string }
	e.must(bob, "POST", "/guilds/"+guild.ID+"/invites", map[string]int{"max_uses": 5}, &bobInvite)
	e.must(alice, "POST", "/guilds/"+guild.ID+"/invites", map[string]int{"max_age": 3600}, &aliceInvite)
	if status := e.call(bob, "GET", "/guilds/"+guild.ID+"/invites", nil, nil); status != http.StatusForbidden {
		t.Fatalf("bob listing invites: status %d", status)
	}
	var invites []struct {
		Code      string
		MaxUses   int     `json:"max_uses"`
		ExpiresAt *string `json:"expires_at"`
		Creator   struct{ Username string }
	}
	e.must(alice, "GET", "/guilds/"+guild.ID+"/invites", nil, &invites)
	found := map[string]string{}
	for _, inv := range invites {
		found[inv.Code] = inv.Creator.Username
	}
	if len(invites) != 3 || found[bobInvite.Code] != "bob" || found[aliceInvite.Code] != "alice" {
		t.Fatalf("invites: %+v", invites)
	}
	if status := e.call(bob, "DELETE", "/invites/"+aliceInvite.Code, nil, nil); status != http.StatusForbidden {
		t.Fatalf("bob revoking alice's invite: status %d", status)
	}
	if dave, _ := e.login("dave"); e.call(dave, "DELETE", "/invites/"+bobInvite.Code, nil, nil) != http.StatusNotFound {
		t.Fatal("outsider revoked an invite")
	}
	e.must(bob, "DELETE", "/invites/"+bobInvite.Code, nil, nil)
	e.must(alice, "DELETE", "/invites/"+aliceInvite.Code, nil, nil)
	if status := e.call(alice, "DELETE", "/invites/"+aliceInvite.Code, nil, nil); status != http.StatusNotFound {
		t.Fatalf("revoking twice: status %d", status)
	}
	e.must(alice, "GET", "/guilds/"+guild.ID+"/invites", nil, &invites)
	if len(invites) != 1 || invites[0].Code != invite.Code {
		t.Fatalf("invites after revoking: %+v", invites)
	}
	if status := e.call(bob, "GET", "/guilds/"+guild.ID+"/bans", nil, nil); status != http.StatusForbidden {
		t.Fatalf("bob listing bans: status %d", status)
	}

	// Banned: Bob is told, loses access, and a fresh invite does not let him back in.
	e.must(alice, "PUT", "/guilds/"+guild.ID+"/bans/"+bobID, nil, nil)
	var bans []struct{ User struct{ ID string } }
	if e.must(alice, "GET", "/guilds/"+guild.ID+"/bans", nil, &bans); len(bans) != 1 || bans[0].User.ID != bobID {
		t.Fatalf("bans: %+v", bans)
	}
	bobWS.next("GUILD_DELETE", nil)
	if status := e.call(bob, "GET", "/channels/"+general+"/messages", nil, nil); status != http.StatusNotFound {
		t.Fatalf("banned bob reading: status %d", status)
	}
	e.must(alice, "POST", "/guilds/"+guild.ID+"/invites", map[string]int{}, &invite)
	if status := e.call(bob, "POST", "/invites/"+invite.Code, nil, nil); status != http.StatusForbidden {
		t.Fatalf("banned bob rejoining: status %d", status)
	}
	e.must(alice, "DELETE", "/guilds/"+guild.ID+"/bans/"+bobID, nil, nil)
	if e.must(alice, "GET", "/guilds/"+guild.ID+"/bans", nil, &bans); len(bans) != 0 {
		t.Fatalf("bans after unban: %+v", bans)
	}

	// Junk IDs are a 404, not a database error.
	if status := e.call(alice, "DELETE", "/channels/not-a-uuid", nil, nil); status != http.StatusNotFound {
		t.Fatalf("junk id: status %d", status)
	}
}

// upload sends a file the way the client does and returns the attachment ID.
func (e env) upload(token, channelID, filename string, content []byte) string {
	e.t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, _ := form.CreateFormFile("file", filename)
	part.Write(content)
	form.Close()
	req, _ := http.NewRequest("POST", e.url+"/channels/"+channelID+"/attachments", &body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", form.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct{ ID string }
	if json.NewDecoder(resp.Body).Decode(&out); resp.StatusCode != http.StatusOK || out.ID == "" {
		e.t.Fatalf("upload: status %d", resp.StatusCode)
	}
	return out.ID
}

// download returns the status and body of an attachment.
func (e env) download(token, attachmentID string) (int, []byte) {
	e.t.Helper()
	req, _ := http.NewRequest("GET", e.url+"/attachments/"+attachmentID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func TestSocialFlow(t *testing.T) {
	e := start(t)
	alice, aliceID := e.login("alice")
	bob, bobID := e.login("bob")
	carol, _ := e.login("carol")
	aliceWS, bobWS := e.connect(alice), e.connect(bob)
	aliceWS.next("READY", nil)
	bobWS.next("READY", nil)

	// Friend request by username, then accept.
	type rel struct {
		User   struct{ ID string }
		Status int
		Online bool
	}
	var r rel
	e.must(alice, "POST", "/relationships", map[string]string{"username": "bob"}, nil)
	if bobWS.next("RELATIONSHIP_UPDATE", &r); r.User.ID != aliceID || r.Status != 2 {
		t.Fatalf("bob's incoming request: %+v", r)
	}
	if status := e.call(carol, "PUT", "/relationships/"+aliceID, nil, nil); status != http.StatusNotFound {
		t.Fatalf("carol accepting a request that does not exist: status %d", status)
	}
	e.must(bob, "PUT", "/relationships/"+aliceID, nil, nil)
	aliceWS.next("RELATIONSHIP_UPDATE", nil) // her own outgoing request
	if aliceWS.next("RELATIONSHIP_UPDATE", &r); r.User.ID != bobID || r.Status != 1 || !r.Online {
		t.Fatalf("alice after accept: %+v", r)
	}

	// A DM carries a message with an attachment, stored in object storage.
	var dm struct{ ID string }
	e.must(alice, "POST", "/dms", map[string]string{"recipient_id": bobID}, &dm)
	bobWS.next("DM_CREATE", nil)
	content := []byte("\x89PNG\r\n\x1a\n not really a png")
	fileID := e.upload(alice, dm.ID, `..\..\evil/foto.png`, content)
	if status, _ := e.download(bob, fileID); status != http.StatusNotFound {
		t.Fatalf("bob reading an upload that was not sent yet: status %d", status)
	}
	if status := e.call(bob, "POST", "/channels/"+dm.ID+"/messages", map[string]any{"attachments": []string{fileID}}, nil); status != http.StatusBadRequest {
		t.Fatalf("bob sending alice's upload as his: status %d", status)
	}
	type message struct {
		ID          string
		Content     string
		Mentions    []string
		EditedAt    *time.Time `json:"edited_at"`
		Attachments []struct {
			ID, Filename string
			ContentType  string `json:"content_type"`
			Size         int64
		}
		Reactions []struct {
			Emoji string
			Users []string
		}
	}
	var msg message
	e.must(alice, "POST", "/channels/"+dm.ID+"/messages", map[string]any{"content": "olha <@" + bobID + ">", "attachments": []string{fileID}}, nil)
	bobWS.next("MESSAGE_CREATE", &msg)
	if len(msg.Attachments) != 1 || msg.Attachments[0].Filename != "foto.png" || msg.Attachments[0].ContentType != "image/png" ||
		msg.Attachments[0].Size != int64(len(content)) || len(msg.Mentions) != 1 || msg.Mentions[0] != bobID {
		t.Fatalf("message with attachment: %+v", msg)
	}
	if status, got := e.download(bob, fileID); status != http.StatusOK || !bytes.Equal(got, content) {
		t.Fatalf("bob downloading: status %d, %d bytes", status, len(got))
	}

	// Outsiders see nothing of the DM.
	if status, _ := e.download(carol, fileID); status != http.StatusNotFound {
		t.Fatalf("carol downloading: status %d", status)
	}
	if status := e.call(carol, "GET", "/channels/"+dm.ID+"/messages", nil, nil); status != http.StatusNotFound {
		t.Fatalf("carol reading the DM: status %d", status)
	}

	// Reactions, edits and typing reach the other side.
	path := "/channels/" + dm.ID + "/messages/" + msg.ID
	e.must(bob, "PUT", path+"/reactions/"+url.PathEscape("👍"), nil, nil)
	aliceWS.next("REACTION_ADD", nil)
	if status := e.call(bob, "PUT", path+"/reactions/texto", nil, nil); status != http.StatusBadRequest {
		t.Fatalf("text as a reaction: status %d", status)
	}
	if status := e.call(bob, "PATCH", path, map[string]string{"content": "hackeado"}, nil); status != http.StatusForbidden {
		t.Fatalf("bob editing alice's message: status %d", status)
	}
	e.must(alice, "PATCH", path, map[string]string{"content": "editado"}, nil)
	if bobWS.next("MESSAGE_UPDATE", &msg); msg.Content != "editado" || msg.EditedAt == nil || len(msg.Mentions) != 0 {
		t.Fatalf("edited message: %+v", msg)
	}
	var history []message
	e.must(bob, "GET", "/channels/"+dm.ID+"/messages", nil, &history)
	if len(history) != 1 || len(history[0].Reactions) != 1 || history[0].Reactions[0].Emoji != "👍" || history[0].Reactions[0].Users[0] != bobID {
		t.Fatalf("history: %+v", history)
	}
	e.must(bob, "POST", "/channels/"+dm.ID+"/typing", nil, nil)
	aliceWS.next("TYPING_START", nil)

	// Groups are for friends only.
	if status := e.call(alice, "POST", "/dms/groups", map[string]any{"recipient_ids": []string{bobID, e.userID(carol)}}, nil); status != http.StatusForbidden {
		t.Fatalf("group with a non-friend: status %d", status)
	}
	var group struct{ ID string }
	e.must(alice, "POST", "/dms/groups", map[string]any{"recipient_ids": []string{bobID}}, &group)
	if status := e.call(alice, "POST", "/dms/"+group.ID+"/recipients", map[string]any{"recipient_ids": []string{bobID, e.userID(carol)}}, nil); status != http.StatusForbidden {
		t.Fatalf("adding a non-friend to a group: status %d", status)
	}

	// Blocking: nothing new gets through, the block is not announced, and a new request looks like a missing user.
	e.must(bob, "PUT", "/blocks/"+aliceID, nil, nil)
	if status := e.call(alice, "POST", "/channels/"+dm.ID+"/messages", map[string]string{"content": "oi?"}, nil); status != http.StatusForbidden {
		t.Fatalf("alice messaging after being blocked: status %d", status)
	}
	if aliceWS.next("RELATIONSHIP_UPDATE", &r); r.Status != 0 {
		t.Fatalf("alice was told status %d, want plain removal", r.Status)
	}
	if status := e.call(alice, "POST", "/relationships", map[string]string{"username": "bob"}, nil); status != http.StatusNotFound {
		t.Fatalf("alice re-adding who blocked her: status %d", status)
	}

	// Deleting the message deletes the file.
	e.must(alice, "DELETE", path, nil, nil)
	bobWS.next("MESSAGE_DELETE", nil)
	if status, _ := e.download(alice, fileID); status != http.StatusNotFound {
		t.Fatalf("attachment after its message was deleted: status %d", status)
	}
}

func (e env) userID(token string) string {
	e.t.Helper()
	var u struct{ ID string }
	e.must(token, "GET", "/users/@me", nil, &u)
	return u.ID
}

func (s socket) send(op string, data any) {
	s.t.Helper()
	if err := wsjson.Write(s.t.Context(), s.c, map[string]any{"op": op, "d": data}); err != nil {
		s.t.Fatal(err)
	}
}

// Media itself is covered by the SFU's own test; this is the call bookkeeping around it.
func TestVoiceFlow(t *testing.T) {
	e := start(t)
	alice, _ := e.login("alice")
	bob, bobID := e.login("bob")
	var guild snapshot
	e.must(alice, "POST", "/guilds", map[string]string{"name": "Oficina"}, &guild)
	text, voiceChannel := guild.Channels[0].ID, guild.Channels[1].ID
	var invite struct{ Code string }
	e.must(alice, "POST", "/guilds/"+guild.ID+"/invites", map[string]int{}, &invite)
	e.must(bob, "POST", "/invites/"+invite.Code, nil, nil)

	aliceWS, bobWS := e.connect(alice), e.connect(bob)
	aliceWS.next("READY", nil)
	bobWS.next("READY", nil)

	type state struct {
		UserID    string `json:"user_id"`
		ChannelID string `json:"channel_id"`
		Muted     bool
	}
	var st state
	var failure struct{ Op, Error string }

	// Only voice channels host calls, and signaling needs a call.
	bobWS.send("voice_join", map[string]string{"channel_id": text})
	if bobWS.next("VOICE_ERROR", &failure); failure.Op != "voice_join" {
		t.Fatalf("joining a text channel: %+v", failure)
	}
	bobWS.send("rtc_offer", map[string]string{"sdp": "x"})
	bobWS.next("VOICE_ERROR", nil)

	// Joining is announced to the others; mute state too.
	var ready struct {
		ChannelID  string `json:"channel_id"`
		Connection struct{ Kind string }
	}
	bobWS.send("voice_join", map[string]string{"channel_id": voiceChannel})
	if bobWS.next("VOICE_READY", &ready); ready.ChannelID != voiceChannel || ready.Connection.Kind != "pion" {
		t.Fatalf("VOICE_READY: %+v", ready)
	}
	if aliceWS.next("VOICE_STATE_UPDATE", &st); st.UserID != bobID || st.ChannelID != voiceChannel {
		t.Fatalf("join state: %+v", st)
	}
	bobWS.send("voice_state", map[string]bool{"deafened": true})
	if aliceWS.next("VOICE_STATE_UPDATE", &st); !st.Muted {
		t.Fatalf("deafened should imply muted: %+v", st)
	}

	// Someone connecting later sees the call in READY.
	var late struct {
		VoiceStates []state `json:"voice_states"`
	}
	e.connect(alice).next("READY", &late)
	if len(late.VoiceStates) != 1 || late.VoiceStates[0].UserID != bobID {
		t.Fatalf("voice states in READY: %+v", late)
	}

	// Being kicked from the guild ends the call; so does dropping the connection.
	e.must(alice, "DELETE", "/guilds/"+guild.ID+"/members/"+bobID, nil, nil)
	if aliceWS.next("VOICE_STATE_UPDATE", &st); st.UserID != bobID || st.ChannelID != "" {
		t.Fatalf("state after kick: %+v", st)
	}
	aliceWS.send("voice_join", map[string]string{"channel_id": voiceChannel})
	aliceWS.next("VOICE_READY", nil)
	aliceWS.next("VOICE_STATE_UPDATE", nil) // the join is published after VOICE_READY; let it pass before the watcher connects
	watcher := e.connect(alice)
	watcher.next("READY", nil)
	aliceWS.c.CloseNow()
	if watcher.next("VOICE_STATE_UPDATE", &st); st.ChannelID != "" {
		t.Fatalf("state after disconnect: %+v", st)
	}
}

// The world is a call with positions: moves go to the others in it, changing room goes to the whole guild.
func TestWorldFlow(t *testing.T) {
	e := start(t)
	alice, aliceID := e.login("alice")
	bob, bobID := e.login("bob")
	var guild snapshot
	e.must(alice, "POST", "/guilds", map[string]string{"name": "Oficina"}, &guild)
	if len(guild.Channels) != 3 || guild.Channels[2].Name != "Mundo" {
		t.Fatalf("a new guild's channels: %+v", guild.Channels)
	}
	voiceChannel, world := guild.Channels[1].ID, guild.Channels[2].ID
	var invite struct{ Code string }
	e.must(alice, "POST", "/guilds/"+guild.ID+"/invites", map[string]int{}, &invite)
	e.must(bob, "POST", "/invites/"+invite.Code, nil, nil)

	// One world per guild: it is neither created nor deleted by hand.
	if status := e.call(alice, "DELETE", "/channels/"+world, nil, nil); status != http.StatusBadRequest {
		t.Fatalf("deleting the world: %d", status)
	}
	if status := e.call(alice, "POST", "/guilds/"+guild.ID+"/channels", map[string]any{"name": "Outro", "type": 4}, nil); status != http.StatusBadRequest {
		t.Fatalf("creating a second world: %d", status)
	}

	var m struct {
		Spawn struct{ X, Y float32 }
		Zones []struct{ ID string }
		Walls []struct{ W float32 }
	}
	if e.must(alice, "GET", "/world/map", nil, &m); len(m.Zones) != 4 || len(m.Walls) == 0 {
		t.Fatalf("map: %+v", m)
	}

	// A picture is asked about every time, and only sent again when the art changed.
	first, err := http.Get(e.url + "/world/sprites/jett")
	if err != nil || first.StatusCode != http.StatusOK || first.Header.Get("ETag") == "" {
		t.Fatalf("sprite: %v %v", first, err)
	}
	first.Body.Close()

	again, _ := http.NewRequest("GET", e.url+"/world/sprites/jett", nil)
	again.Header.Set("If-None-Match", first.Header.Get("ETag"))
	if resp, err := http.DefaultClient.Do(again); err != nil || resp.StatusCode != http.StatusNotModified {
		t.Fatalf("the same sprite again: %v %v", resp, err)
	}

	aliceWS, bobWS := e.connect(alice), e.connect(bob)
	aliceWS.next("READY", nil)
	bobWS.next("READY", nil)

	type state struct {
		UserID string `json:"user_id"`
		X, Y   float32
		Zone   string
	}
	var st state
	var failure struct{ Op, Error string }

	// Positions only exist in the world.
	bobWS.send("voice_join", map[string]string{"channel_id": voiceChannel})
	bobWS.next("VOICE_READY", nil)
	bobWS.send("world_move", map[string]float32{"x": 10, "y": 10})
	if bobWS.next("VOICE_ERROR", &failure); failure.Op != "world_move" {
		t.Fatalf("moving in a voice channel: %+v", failure)
	}

	// Everyone starts at the spawn.
	bobWS.send("voice_join", map[string]string{"channel_id": world})
	bobWS.next("VOICE_READY", nil)
	for st.X == 0 { // skips the states of the voice channel bob just left
		aliceWS.next("VOICE_STATE_UPDATE", &st)
	}
	if st.UserID != bobID || st.X != m.Spawn.X || st.Y != m.Spawn.Y || st.Zone != "" {
		t.Fatalf("join state: %+v", st)
	}

	bobWS.send("world_move", map[string]float32{"x": -5, "y": 10})
	if bobWS.next("VOICE_ERROR", &failure); failure.Op != "world_move" {
		t.Fatalf("moving off the map: %+v", failure)
	}

	// A step in the plaza reaches only who is in the world: alice, outside, sees the next room change first.
	aliceWS.send("voice_join", map[string]string{"channel_id": world})
	aliceWS.next("VOICE_READY", nil)
	bobWS.send("world_move", map[string]float32{"x": 1000, "y": 800})
	var move struct {
		UserID string `json:"user_id"`
		X      float32
	}
	if aliceWS.next("WORLD_MOVE", &move); move.UserID != bobID || move.X != 1000 {
		t.Fatalf("move: %+v", move)
	}

	// Faster than the limit: dropped without an error.
	bobWS.send("world_move", map[string]float32{"x": 1001, "y": 800})
	time.Sleep(60 * time.Millisecond)

	watcher := e.connect(alice) // not in the world
	watcher.next("READY", nil)
	bobWS.send("world_move", map[string]float32{"x": 400, "y": 340})
	if watcher.next("VOICE_STATE_UPDATE", &st); st.UserID != bobID || st.Zone != "valorant" || st.X != 400 {
		t.Fatalf("entering a room: %+v", st)
	}

	// Skins: listed, worn by ID, and only real ones.
	var skins []struct{ ID, Name, Zone string }
	if e.must(bob, "GET", "/world/skins", nil, &skins); len(skins) == 0 || skins[0].ID != "jett" || skins[0].Zone != "valorant" {
		t.Fatalf("skins: %+v", skins)
	}
	bobWS.send("world_skin", map[string]string{"skin": "nope"})
	if bobWS.next("VOICE_ERROR", &failure); failure.Op != "world_skin" {
		t.Fatalf("unknown skin: %+v", failure)
	}
	var worn struct {
		UserID string `json:"user_id"`
		Skin   string
	}
	bobWS.send("world_skin", map[string]string{"skin": "jett"})
	if watcher.next("VOICE_STATE_UPDATE", &worn); worn.UserID != bobID || worn.Skin != "jett" {
		t.Fatalf("wearing a skin: %+v", worn)
	}

	// Reactions reach the room: alice walks in with bob, and gets his; an unknown one is refused.
	aliceWS.send("world_move", map[string]float32{"x": 400, "y": 340})
	for st.UserID != aliceID || st.Zone != "valorant" {
		bobWS.next("VOICE_STATE_UPDATE", &st)
	}
	bobWS.send("world_react", map[string]string{"emoji": "nope"})
	if bobWS.next("VOICE_ERROR", &failure); failure.Op != "world_react" {
		t.Fatalf("unknown reaction: %+v", failure)
	}
	var react struct {
		UserID string `json:"user_id"`
		Emoji  string
	}
	bobWS.send("world_react", map[string]string{"emoji": "🔥"})
	if aliceWS.next("WORLD_REACT", &react); react.UserID != bobID || react.Emoji != "🔥" {
		t.Fatalf("reaction: %+v", react)
	}
}
