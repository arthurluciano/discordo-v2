package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"

	"gonativeui/app/secret"
	"gonativeui/app/shell"
)

// Client talks to the Discordo server: REST with the session token as a Bearer, and the gateway.
// Every method blocks: call them from goroutines, never from Build.
type Client struct {
	Server string // e.g. https://discordo.example.com
	Token  string
}

var errUnauthorized = errors.New("sessão expirada")

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var r *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}

		r = bytes.NewReader(b)
	} else {
		r = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.Server+path, r)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return errUnauthorized
	}

	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		json.NewDecoder(resp.Body).Decode(&e)

		return fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, e.Error)
	}

	if out == nil {
		return nil
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

// pageSize is how many messages a page brings: fewer means the channel has no older ones.
const pageSize = 50

// Messages are a page of a channel's messages before the message before ("": the newest), newest first.
func (c *Client) Messages(ctx context.Context, channelID, before string) ([]Message, error) {
	path := fmt.Sprintf("/channels/%s/messages?limit=%d", url.PathEscape(channelID), pageSize)
	if before != "" {
		path += "&before=" + url.QueryEscape(before)
	}

	var msgs []Message
	err := c.do(ctx, "GET", path, nil, &msgs)

	return msgs, err
}

func (c *Client) Send(ctx context.Context, channelID, content string) (Message, error) {
	var m Message
	err := c.do(ctx, "POST", "/channels/"+url.PathEscape(channelID)+"/messages", map[string]string{"content": content}, &m)

	return m, err
}

// React adds (or with add false removes) my reaction.
func (c *Client) React(ctx context.Context, channelID, messageID, emoji string, add bool) error {
	method := "PUT"
	if !add {
		method = "DELETE"
	}

	return c.do(ctx, method, "/channels/"+url.PathEscape(channelID)+"/messages/"+url.PathEscape(messageID)+"/reactions/"+url.PathEscape(emoji), nil, nil)
}

// maxImage bounds a downloaded picture: the server takes images up to 8 MB and files up to 25.
const maxImage = 26 << 20

// Fetch downloads an avatar, icon or attachment. Paths on our server ("/images/…", "/attachments/…") carry the session.
func (c *Client) Fetch(ctx context.Context, ref string) ([]byte, error) {
	target, own := ref, strings.HasPrefix(ref, "/")
	if own {
		target = c.Server + ref
	}

	req, err := http.NewRequestWithContext(ctx, "GET", target, nil)
	if err != nil {
		return nil, err
	}

	if own {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %d", ref, resp.StatusCode)
	}

	return io.ReadAll(io.LimitReader(resp.Body, maxImage))
}

// Upload sends a file as multipart "file" to path (POST /images, POST /channels/{id}/attachments) and decodes the answer.
func (c *Client) Upload(ctx context.Context, path, filename string, data []byte, out any) error {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return err
	}

	part.Write(data)
	w.Close()

	req, err := http.NewRequestWithContext(ctx, "POST", c.Server+path, &body)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return errUnauthorized
	}

	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		json.NewDecoder(resp.Body).Decode(&e)

		return fmt.Errorf("POST %s: %d %s", path, resp.StatusCode, e.Error)
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) Logout(ctx context.Context) error {
	return c.do(ctx, "POST", "/auth/logout", nil, nil)
}

// Gateway hands each event to on until the connection drops or ctx ends. errUnauthorized means the token is no good.
func (c *Client) Gateway(ctx context.Context, on func(Event)) error {
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(c.Server, "http")+"/gateway", nil)
	if err != nil {
		return err
	}
	defer ws.CloseNow()

	ws.SetReadLimit(8 << 20) // READY carries every guild

	hello, _ := json.Marshal(map[string]string{"token": c.Token})
	if err := ws.Write(ctx, websocket.MessageText, hello); err != nil {
		return err
	}

	for {
		_, data, err := ws.Read(ctx)
		if websocket.CloseStatus(err) == websocket.StatusPolicyViolation {
			return errUnauthorized
		}

		if err != nil {
			return err
		}

		var e Event
		if json.Unmarshal(data, &e) != nil {
			continue
		}

		on(e)
	}
}

// Login opens the browser on the server's Discord login and waits for the token on a loopback port (RFC 8252).
func Login(ctx context.Context, server string) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer ln.Close()

	nonce := randomString()
	tokens := make(chan string, 1)

	srv := &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if r.URL.Path != "/callback" || q.Get("nonce") != nonce || q.Get("token") == "" {
				http.Error(w, "Login inválido.", http.StatusBadRequest)
				return
			}

			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, "<!doctype html><meta charset=utf-8><title>Discordo</title><p style='font:16px system-ui;margin:3em'>%s</p>",
				html.EscapeString("Pronto! Pode fechar esta aba e voltar ao Discordo."))

			select {
			case tokens <- q.Get("token"):
			default:
			}
		}),
	}

	go srv.Serve(ln)
	defer srv.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	if err := shell.OpenURL(fmt.Sprintf("%s/auth/discord/login?port=%d&nonce=%s", server, port, nonce)); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	select {
	case t := <-tokens:
		return t, nil
	case <-ctx.Done():
		return "", errors.New("o login não terminou a tempo")
	}
}

func randomString() string {
	b := make([]byte, 24)
	rand.Read(b)

	return base64.RawURLEncoding.EncodeToString(b)
}

// The session token lives in the system's credential store (the Keychain on macOS), one per server.
var tokens = secret.Open("br.com.discordo.desktop")

func loadToken(server string) string {
	b, err := tokens.Get(server)
	if err != nil {
		return ""
	}

	return string(b)
}

func saveToken(server, token string) error {
	if token == "" {
		err := tokens.Delete(server)
		if errors.Is(err, secret.ErrNotFound) {
			return nil
		}

		return err
	}

	return tokens.Set(server, []byte(token))
}
