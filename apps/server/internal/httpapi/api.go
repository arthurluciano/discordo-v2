// Package httpapi is the REST surface: it decodes requests, calls services and maps errors to status codes.
package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"discordo/server/internal/auth"
	"discordo/server/internal/dm"
	"discordo/server/internal/errs"
	"discordo/server/internal/guild"
	"discordo/server/internal/id"
	"discordo/server/internal/image"
	"discordo/server/internal/message"
	"discordo/server/internal/permission"
	"discordo/server/internal/relation"
	"discordo/server/internal/user"
)

const oauthCookie = "discordo_oauth"

type API struct {
	Auth          *auth.Service
	Guilds        *guild.Service
	Messages      *message.Service
	Relations     *relation.Service
	DMs           *dm.Service
	Users         *user.Service
	Images        *image.Service
	Gateway       http.Handler
	Web           http.Handler // nil: no web app (API only)
	SecureCookies bool
}

// handler is an authenticated endpoint. A nil result means 204.
type handler func(r *http.Request, u user.User) (any, error)

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /auth/discord/login", a.login)
	mux.HandleFunc("GET /auth/discord/callback", a.callback)
	mux.Handle("GET /gateway", a.Gateway)
	if a.Web != nil {
		mux.Handle("GET /", a.Web) // the most general pattern: every API route above and below wins over it
	}

	h := func(pattern string, fn handler) { mux.HandleFunc(pattern, a.authed(1<<20, fn)) }
	g, m, rel, dms := a.Guilds, a.Messages, a.Relations, a.DMs

	mux.HandleFunc("POST /auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			fail(w, errs.ErrForbidden)
			return
		}
		if err := a.Auth.Logout(r.Context(), sessionToken(r)); err != nil {
			fail(w, err)
			return
		}
		a.setSession(w, "", -1)
		w.WriteHeader(http.StatusNoContent)
	})
	h("GET /users/@me", func(_ *http.Request, u user.User) (any, error) { return u, nil })
	h("PATCH /users/@me", func(r *http.Request, u user.User) (any, error) {
		var in user.ProfileInput
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		return a.Users.UpdateProfile(r.Context(), u.ID, in)
	})
	h("GET /users/{user}/profile", func(r *http.Request, u user.User) (any, error) {
		return a.Users.Profile(r.Context(), u.ID, r.PathValue("user"))
	})

	// multipart/form-data with one part named "file"; returns {"url"} for an avatar, icon or banner.
	mux.HandleFunc("POST /images", a.authed(image.MaxSize+1<<20, func(r *http.Request, _ user.User) (any, error) {
		part, err := filePart(r)
		if err != nil {
			return nil, err
		}
		url, err := a.Images.Upload(r.Context(), part)
		return map[string]string{"url": url}, err
	}))
	// Public: avatars and icons are shown to anyone who sees the user or server, like any profile picture.
	mux.HandleFunc("GET /images/{image}", func(w http.ResponseWriter, r *http.Request) {
		content, contentType, err := a.Images.Open(r.Context(), r.PathValue("image"))
		if err != nil {
			fail(w, err)
			return
		}
		defer content.Close()
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // an image never changes; a new one gets a new ID
		http.ServeContent(w, r, "", time.Time{}, content)
	})

	h("POST /guilds", func(r *http.Request, u user.User) (any, error) {
		var in struct{ Name string }
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		return g.Create(r.Context(), u.ID, in.Name)
	})
	h("PATCH /guilds/{guild}", func(r *http.Request, u user.User) (any, error) {
		var in guild.GuildInput
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		return nil, g.Update(r.Context(), u.ID, r.PathValue("guild"), in)
	})
	h("DELETE /guilds/{guild}", func(r *http.Request, u user.User) (any, error) {
		return nil, g.Delete(r.Context(), u.ID, r.PathValue("guild"))
	})

	h("DELETE /guilds/{guild}/members/@me", func(r *http.Request, u user.User) (any, error) {
		return nil, g.Leave(r.Context(), u.ID, r.PathValue("guild"))
	})
	h("DELETE /guilds/{guild}/members/{user}", func(r *http.Request, u user.User) (any, error) {
		return nil, g.Kick(r.Context(), u.ID, r.PathValue("guild"), r.PathValue("user"))
	})
	h("PUT /guilds/{guild}/bans/{user}", func(r *http.Request, u user.User) (any, error) {
		return nil, g.Ban(r.Context(), u.ID, r.PathValue("guild"), r.PathValue("user"))
	})
	h("GET /guilds/{guild}/bans", func(r *http.Request, u user.User) (any, error) {
		return g.Bans(r.Context(), u.ID, r.PathValue("guild"))
	})
	h("DELETE /guilds/{guild}/bans/{user}", func(r *http.Request, u user.User) (any, error) {
		return nil, g.Unban(r.Context(), u.ID, r.PathValue("guild"), r.PathValue("user"))
	})
	h("PUT /guilds/{guild}/members/{user}/roles/{role}", func(r *http.Request, u user.User) (any, error) {
		return nil, g.SetMemberRole(r.Context(), u.ID, r.PathValue("guild"), r.PathValue("user"), r.PathValue("role"), true)
	})
	h("DELETE /guilds/{guild}/members/{user}/roles/{role}", func(r *http.Request, u user.User) (any, error) {
		return nil, g.SetMemberRole(r.Context(), u.ID, r.PathValue("guild"), r.PathValue("user"), r.PathValue("role"), false)
	})

	h("POST /guilds/{guild}/roles", func(r *http.Request, u user.User) (any, error) {
		var in guild.RoleInput
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		return g.CreateRole(r.Context(), u.ID, r.PathValue("guild"), in)
	})
	h("PATCH /guilds/{guild}/roles/{role}", func(r *http.Request, u user.User) (any, error) {
		var in guild.RoleInput
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		return g.UpdateRole(r.Context(), u.ID, r.PathValue("guild"), r.PathValue("role"), in)
	})
	h("POST /guilds/{guild}/roles/{role}/move", func(r *http.Request, u user.User) (any, error) {
		var in struct {
			Up bool `json:"up"`
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		return nil, g.MoveRole(r.Context(), u.ID, r.PathValue("guild"), r.PathValue("role"), in.Up)
	})
	h("DELETE /guilds/{guild}/roles/{role}", func(r *http.Request, u user.User) (any, error) {
		return nil, g.DeleteRole(r.Context(), u.ID, r.PathValue("guild"), r.PathValue("role"))
	})

	h("POST /guilds/{guild}/invites", func(r *http.Request, u user.User) (any, error) {
		var in struct {
			MaxUses int `json:"max_uses"`
			MaxAge  int `json:"max_age"` // seconds; 0 = never expires
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		return g.CreateInvite(r.Context(), u.ID, r.PathValue("guild"), in.MaxUses, time.Duration(in.MaxAge)*time.Second)
	})
	h("GET /guilds/{guild}/invites", func(r *http.Request, u user.User) (any, error) {
		return g.Invites(r.Context(), u.ID, r.PathValue("guild"))
	})
	h("DELETE /invites/{code}", func(r *http.Request, u user.User) (any, error) {
		return nil, g.RevokeInvite(r.Context(), u.ID, r.PathValue("code"))
	})
	h("POST /invites/{code}", func(r *http.Request, u user.User) (any, error) {
		return g.Join(r.Context(), u.ID, r.PathValue("code"))
	})

	h("POST /guilds/{guild}/channels", func(r *http.Request, u user.User) (any, error) {
		var in guild.ChannelInput
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		return g.CreateChannel(r.Context(), u.ID, r.PathValue("guild"), in)
	})
	h("PATCH /channels/{channel}", func(r *http.Request, u user.User) (any, error) {
		var in guild.ChannelInput
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		return g.UpdateChannel(r.Context(), u.ID, r.PathValue("channel"), in)
	})
	h("DELETE /channels/{channel}", func(r *http.Request, u user.User) (any, error) {
		return nil, g.DeleteChannel(r.Context(), u.ID, r.PathValue("channel"))
	})
	h("PUT /channels/{channel}/overwrites/{target}", func(r *http.Request, u user.User) (any, error) {
		var in permission.Overwrite
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		in.TargetID = r.PathValue("target")
		return nil, g.SetOverwrite(r.Context(), u.ID, r.PathValue("channel"), in)
	})
	h("DELETE /channels/{channel}/overwrites/{target}", func(r *http.Request, u user.User) (any, error) {
		return nil, g.DeleteOverwrite(r.Context(), u.ID, r.PathValue("channel"), r.PathValue("target"))
	})

	h("GET /channels/{channel}/messages", func(r *http.Request, u user.User) (any, error) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		return m.List(r.Context(), u.ID, r.PathValue("channel"), r.URL.Query().Get("before"), limit)
	})
	h("POST /channels/{channel}/messages", func(r *http.Request, u user.User) (any, error) {
		var in struct {
			Content     string
			Attachments []string
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		return m.Send(r.Context(), u.ID, r.PathValue("channel"), in.Content, in.Attachments)
	})
	h("PATCH /channels/{channel}/messages/{message}", func(r *http.Request, u user.User) (any, error) {
		var in struct{ Content string }
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		return m.Edit(r.Context(), u.ID, r.PathValue("channel"), r.PathValue("message"), in.Content)
	})
	h("DELETE /channels/{channel}/messages/{message}", func(r *http.Request, u user.User) (any, error) {
		return nil, m.Delete(r.Context(), u.ID, r.PathValue("channel"), r.PathValue("message"))
	})
	h("PUT /channels/{channel}/messages/{message}/reactions/{emoji}", func(r *http.Request, u user.User) (any, error) {
		return nil, m.React(r.Context(), u.ID, r.PathValue("channel"), r.PathValue("message"), r.PathValue("emoji"), true)
	})
	h("DELETE /channels/{channel}/messages/{message}/reactions/{emoji}", func(r *http.Request, u user.User) (any, error) {
		return nil, m.React(r.Context(), u.ID, r.PathValue("channel"), r.PathValue("message"), r.PathValue("emoji"), false)
	})
	h("POST /channels/{channel}/typing", func(r *http.Request, u user.User) (any, error) {
		return nil, m.Typing(r.Context(), u.ID, r.PathValue("channel"))
	})

	// multipart/form-data with one part named "file"; the returned ID goes in a message's "attachments".
	mux.HandleFunc("POST /channels/{channel}/attachments", a.authed(message.MaxUploadSize+1<<20, func(r *http.Request, u user.User) (any, error) {
		part, err := filePart(r)
		if err != nil {
			return nil, err
		}
		return m.Upload(r.Context(), u.ID, r.PathValue("channel"), part.FileName(), part)
	}))
	h("GET /attachments/{attachment}", func(r *http.Request, u user.User) (any, error) {
		att, content, err := m.OpenAttachment(r.Context(), u.ID, r.PathValue("attachment"))
		return file{att, content}, err
	})

	h("POST /relationships", func(r *http.Request, u user.User) (any, error) {
		var in struct{ Username string }
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		return nil, rel.Request(r.Context(), u.ID, in.Username)
	})
	h("PUT /relationships/{user}", func(r *http.Request, u user.User) (any, error) {
		return nil, rel.Accept(r.Context(), u.ID, r.PathValue("user"))
	})
	h("DELETE /relationships/{user}", func(r *http.Request, u user.User) (any, error) {
		return nil, rel.Remove(r.Context(), u.ID, r.PathValue("user"))
	})
	h("PUT /blocks/{user}", func(r *http.Request, u user.User) (any, error) {
		return nil, rel.Block(r.Context(), u.ID, r.PathValue("user"))
	})

	h("POST /dms", func(r *http.Request, u user.User) (any, error) {
		var in struct {
			RecipientID string `json:"recipient_id"`
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		if !id.Valid(in.RecipientID) {
			return nil, errs.Invalid("recipient_id must be a user ID")
		}
		return dms.Open(r.Context(), u.ID, in.RecipientID)
	})
	h("POST /dms/groups", func(r *http.Request, u user.User) (any, error) {
		var in struct {
			Name         string
			RecipientIDs []string `json:"recipient_ids"`
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		return dms.CreateGroup(r.Context(), u.ID, in.Name, in.RecipientIDs)
	})
	h("POST /dms/{channel}/recipients", func(r *http.Request, u user.User) (any, error) {
		var in struct {
			RecipientIDs []string `json:"recipient_ids"`
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		return nil, dms.AddRecipients(r.Context(), u.ID, r.PathValue("channel"), in.RecipientIDs)
	})
	h("DELETE /dms/{channel}/recipients/@me", func(r *http.Request, u user.User) (any, error) {
		return nil, dms.Leave(r.Context(), u.ID, r.PathValue("channel"))
	})
	return mux
}

// file is a handler result that is streamed instead of JSON-encoded.
type file struct {
	message.Attachment
	content io.ReadSeekCloser
}

// Auth endpoints

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	target, state, verifier := a.Auth.Begin()
	// The browser carries the flow state; Lax lets it come back on the provider's redirect.
	http.SetCookie(w, &http.Cookie{
		Name: oauthCookie, Value: state + "." + verifier, Path: "/auth", MaxAge: 600,
		HttpOnly: true, Secure: a.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, target, http.StatusFound)
}

func (a *API) callback(w http.ResponseWriter, r *http.Request) {
	// A person is looking at this response: failures go back to the app's login screen, which explains them.
	back := func(reason string) { http.Redirect(w, r, "/?login_error="+reason, http.StatusFound) }
	c, err := r.Cookie(oauthCookie)
	if err != nil {
		back("expired")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Path: "/auth", MaxAge: -1})
	state, verifier, _ := strings.Cut(c.Value, ".")
	q := r.URL.Query()
	if q.Get("code") == "" || subtle.ConstantTimeCompare([]byte(state), []byte(q.Get("state"))) != 1 {
		back("cancelled")
		return
	}
	token, _, err := a.Auth.Callback(r.Context(), q.Get("code"), verifier)
	if err != nil {
		slog.Warn("login failed", "err", err)
		back("failed")
		return
	}
	a.setSession(w, token, int(auth.SessionTTL.Seconds()))
	http.Redirect(w, r, "/", http.StatusFound)
}

// setSession sets (or with maxAge < 0 clears) the session cookie the web app runs on.
func (a *API) setSession(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: auth.SessionCookie, Value: token, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: a.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
}

// Plumbing

// filePart returns the "file" part of a multipart/form-data body.
func filePart(r *http.Request) (*multipart.Part, error) {
	parts, err := r.MultipartReader()
	if err != nil {
		return nil, errs.Invalid("expected multipart/form-data")
	}
	for {
		part, err := parts.NextPart()
		if err != nil {
			return nil, errs.Invalid(`missing "file" part`)
		}
		if part.FormName() == "file" {
			return part, nil
		}
	}
}

func (a *API) authed(maxBody int64, fn handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := a.Auth.Authenticate(r.Context(), sessionToken(r))
		if err != nil {
			fail(w, err)
			return
		}
		if !sameOrigin(r) {
			fail(w, errs.ErrForbidden)
			return
		}
		// Every ID in a path is a UUID; rejecting junk here keeps it from reaching SQL as a cast error.
		for _, name := range []string{"guild", "channel", "user", "role", "target", "message", "attachment"} {
			if v := r.PathValue(name); v != "" && !id.Valid(v) {
				fail(w, errs.ErrNotFound)
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		out, err := fn(r, u)
		if err != nil {
			fail(w, err)
			return
		}
		switch out := out.(type) {
		case nil:
			w.WriteHeader(http.StatusNoContent)
		case file:
			defer out.content.Close()
			w.Header().Set("Content-Type", out.ContentType)
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": out.Filename}))
			http.ServeContent(w, r, "", time.Time{}, out.content)
		default:
			write(w, http.StatusOK, out)
		}
	}
}

// sessionToken is the bearer token when there is one (API clients), else the browser's session cookie.
func sessionToken(r *http.Request) string {
	if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return token
	}
	if c, err := r.Cookie(auth.SessionCookie); err == nil {
		return c.Value
	}
	return ""
}

// sameOrigin guards cookie-authenticated writes against cross-site requests (CSRF): browsers
// always send Origin on them. Bearer requests carry no ambient credentials, so they pass.
func sameOrigin(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Header.Get("Authorization") != "" {
		return true
	}
	origin, err := url.Parse(r.Header.Get("Origin"))
	return err == nil && origin.Host == r.Host
}

func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return errs.Invalid("malformed JSON body")
	}
	return nil
}

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	for sentinel, code := range map[error]int{
		errs.ErrNotFound:     http.StatusNotFound,
		errs.ErrForbidden:    http.StatusForbidden,
		errs.ErrInvalid:      http.StatusBadRequest,
		errs.ErrUnauthorized: http.StatusUnauthorized,
		errs.ErrConflict:     http.StatusConflict,
	} {
		if errors.Is(err, sentinel) {
			status = code
		}
	}
	msg := err.Error()
	if status == http.StatusInternalServerError {
		slog.Error("request failed", "err", err)
		msg = "internal error" // never leak internals to clients
	} else if status == http.StatusUnauthorized {
		msg = "unauthorized" // the wrapped provider error is for logs only
	}
	write(w, status, map[string]string{"error": msg})
}
