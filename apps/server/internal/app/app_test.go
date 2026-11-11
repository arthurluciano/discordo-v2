package app_test

import (
	"bytes"
	"context"
	"crypto/sha256"
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

// login walks the browser + app flow and returns the one-time login code and its PKCE verifier.
func (e env) loginCode(name string) (code, verifier string) {
	e.t.Helper()
	verifier = "verifier-of-" + name
	sum := sha256.Sum256([]byte(verifier))
	browser := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	resp, err := browser.Get(e.url + "/auth/discord/login?port=4242&challenge=" + base64.RawURLEncoding.EncodeToString(sum[:]))
	if err != nil || resp.StatusCode != http.StatusFound {
		e.t.Fatalf("login: %v %v", err, resp)
	}
	toProvider, _ := url.Parse(resp.Header.Get("Location"))

	req, _ := http.NewRequest("GET", e.url+"/auth/discord/callback?code="+name+"&state="+toProvider.Query().Get("state"), nil)
	for _, c := range resp.Cookies() {
		req.AddCookie(c)
	}
	resp, err = browser.Do(req)
	if err != nil || resp.StatusCode != http.StatusFound {
		e.t.Fatalf("callback: %v %v", err, resp)
	}
	toApp, _ := url.Parse(resp.Header.Get("Location"))
	if toApp.Scheme != "http" || toApp.Host != "127.0.0.1:4242" {
		e.t.Fatalf("callback redirected to %s", toApp)
	}
	return toApp.Query().Get("code"), verifier
}

func (e env) login(name string) (token, userID string) {
	e.t.Helper()
	code, verifier := e.loginCode(name)
	var out struct {
		Token string
		User  struct{ ID string }
	}
	e.must("", "POST", "/auth/token", map[string]string{"code": code, "verifier": verifier}, &out)
	return out.Token, out.User.ID
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
	Channels []struct{ ID, Name string }
	Members  []struct {
		User   struct{ ID string }
		Online bool
	}
}

func TestLoginRejectsWrongVerifierAndReuse(t *testing.T) {
	e := start(t)
	code, verifier := e.loginCode("mallory")
	if status := e.call("", "POST", "/auth/token", map[string]string{"code": code, "verifier": "stolen"}, nil); status != http.StatusUnauthorized {
		t.Fatalf("wrong verifier: status %d", status)
	}
	// The failed attempt burned the code, so even the right verifier is too late.
	if status := e.call("", "POST", "/auth/token", map[string]string{"code": code, "verifier": verifier}, nil); status != http.StatusUnauthorized {
		t.Fatalf("reused code: status %d", status)
	}
	if status := e.call("not-a-token", "GET", "/users/@me", nil, nil); status != http.StatusUnauthorized {
		t.Fatalf("bad token: status %d", status)
	}
}

func TestGuildRealtimeFlow(t *testing.T) {
	e := start(t)
	alice, _ := e.login("alice")
	bob, bobID := e.login("bob")

	// Alice creates a guild and invites Bob.
	var guild snapshot
	e.must(alice, "POST", "/guilds", map[string]string{"name": "Oficina"}, &guild)
	if len(guild.Channels) != 2 {
		t.Fatalf("new guild has %d channels, want 2", len(guild.Channels))
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

	// Banned: Bob is told, loses access, and a fresh invite does not let him back in.
	e.must(alice, "PUT", "/guilds/"+guild.ID+"/bans/"+bobID, nil, nil)
	bobWS.next("GUILD_DELETE", nil)
	if status := e.call(bob, "GET", "/channels/"+general+"/messages", nil, nil); status != http.StatusNotFound {
		t.Fatalf("banned bob reading: status %d", status)
	}
	e.must(alice, "POST", "/guilds/"+guild.ID+"/invites", map[string]int{}, &invite)
	if status := e.call(bob, "POST", "/invites/"+invite.Code, nil, nil); status != http.StatusForbidden {
		t.Fatalf("banned bob rejoining: status %d", status)
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
	watcher := e.connect(alice)
	watcher.next("READY", nil)
	aliceWS.c.CloseNow()
	if watcher.next("VOICE_STATE_UPDATE", &st); st.ChannelID != "" {
		t.Fatalf("state after disconnect: %+v", st)
	}
}
