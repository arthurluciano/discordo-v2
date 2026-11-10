// Package httpapi is the REST surface: it decodes requests, calls services and maps errors to status codes.
package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
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
	Gateway       http.Handler
	SecureCookies bool
}

// handler is an authenticated endpoint. A nil result means 204.
type handler func(r *http.Request, u user.User) (any, error)

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /auth/discord/login", a.login)
	mux.HandleFunc("GET /auth/discord/callback", a.callback)
	mux.HandleFunc("POST /auth/token", a.token)
	mux.Handle("GET /gateway", a.Gateway)

	h := func(pattern string, fn handler) { mux.HandleFunc(pattern, a.authed(1<<20, fn)) }
	g, m, rel, dms := a.Guilds, a.Messages, a.Relations, a.DMs

	h("POST /auth/logout", func(r *http.Request, _ user.User) (any, error) {
		return nil, a.Auth.Logout(r.Context(), bearer(r))
	})
	h("GET /users/@me", func(_ *http.Request, u user.User) (any, error) { return u, nil })

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
				return m.Upload(r.Context(), u.ID, r.PathValue("channel"), part.FileName(), part)
			}
		}
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
	h("PUT /dms/{channel}/recipients/{user}", func(r *http.Request, u user.User) (any, error) {
		return nil, dms.AddRecipient(r.Context(), u.ID, r.PathValue("channel"), r.PathValue("user"))
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
	challenge := r.URL.Query().Get("challenge")
	// The desktop app listens on this loopback port for the result (RFC 8252). Only the port is
	// client-controlled, so the redirect can never leave the user's own machine.
	port, _ := strconv.Atoi(r.URL.Query().Get("port"))
	if port < 1024 || port > 65535 {
		fail(w, errs.Invalid("port must be the app's loopback port"))
		return
	}
	target, state, verifier, err := a.Auth.Begin(challenge)
	if err != nil {
		fail(w, err)
		return
	}
	// The browser carries the flow state; Lax lets it come back on the provider's redirect.
	http.SetCookie(w, &http.Cookie{
		Name: oauthCookie, Value: state + "." + verifier + "." + challenge + "." + strconv.Itoa(port), Path: "/auth", MaxAge: 600,
		HttpOnly: true, Secure: a.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, target, http.StatusFound)
}

func (a *API) callback(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(oauthCookie)
	if err != nil {
		fail(w, errs.Invalid("login expired, start again from the app"))
		return
	}
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Path: "/auth", MaxAge: -1})
	parts := strings.Split(c.Value, ".")
	q := r.URL.Query()
	if len(parts) != 4 || q.Get("code") == "" || subtle.ConstantTimeCompare([]byte(parts[0]), []byte(q.Get("state"))) != 1 {
		fail(w, errs.Invalid("login was cancelled or its state does not match"))
		return
	}
	loginCode, err := a.Auth.Callback(r.Context(), q.Get("code"), parts[1], parts[2])
	if err != nil {
		fail(w, err)
		return
	}
	http.Redirect(w, r, "http://127.0.0.1:"+parts[3]+"/?code="+url.QueryEscape(loginCode), http.StatusFound)
}

func (a *API) token(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	var in struct{ Code, Verifier string }
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}
	token, u, err := a.Auth.Exchange(r.Context(), in.Code, in.Verifier)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusOK, map[string]any{"token": token, "user": u})
}

// Plumbing

func (a *API) authed(maxBody int64, fn handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := a.Auth.Authenticate(r.Context(), bearer(r))
		if err != nil {
			fail(w, err)
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

func bearer(r *http.Request) string {
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return token
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
