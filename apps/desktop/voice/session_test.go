package voice

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeGateway answers voice_join with the events reply returns, and records the ops it got.
func fakeGateway(t *testing.T, reply func(op string) []string) (string, chan string) {
	ops := make(chan string, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()

		ctx := r.Context()
		if _, _, err := c.Read(ctx); err != nil { // hello
			return
		}

		c.Write(ctx, websocket.MessageText, []byte(`{"t":"READY","d":{"user":{"id":"me"}}}`))
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}

			var msg struct{ Op string }
			json.Unmarshal(data, &msg)
			ops <- msg.Op
			for _, e := range reply(msg.Op) {
				c.Write(ctx, websocket.MessageText, []byte(e))
			}
		}
	}))
	t.Cleanup(srv.Close)

	return srv.URL, ops
}

func waitStatus(t *testing.T, s *Session, want Status) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for ctx.Err() == nil {
		if st, msg := s.Status(); st == want {
			return msg
		}

		time.Sleep(10 * time.Millisecond)
	}

	st, msg := s.Status()
	t.Fatalf("status %v (%q), want %v", st, msg, want)

	return ""
}

func TestSessionFailures(t *testing.T) {
	cases := []struct {
		name, event, want string
	}{
		{"forbidden", `{"t":"VOICE_ERROR","d":{"op":"voice_join","error":"forbidden"}}`, "Erro na chamada: Você não tem permissão para entrar nesta chamada."},
		{"livekit", `{"t":"VOICE_READY","d":{"channel_id":"v1","connection":{"kind":"livekit","url":"wss://x","token":"t"}}}`,
			`Este app não suporta o tipo de conexão de mídia do servidor ("livekit").`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			url, ops := fakeGateway(t, func(op string) []string {
				if op == "voice_join" {
					return []string{c.event}
				}

				return nil
			})

			s := Join(Options{Server: url, Token: "x", ChannelID: "v1", Headless: true})
			if msg := waitStatus(t, s, Failed); msg != c.want {
				t.Fatalf("error %q, want %q", msg, c.want)
			}

			if op := <-ops; op != "voice_join" {
				t.Fatalf("first op %q", op)
			}

			if op := <-ops; op != "voice_leave" { // a failed call hangs up
				t.Fatalf("then %q, want voice_leave", op)
			}
		})
	}
}

func TestSessionMovedElsewhere(t *testing.T) {
	url, _ := fakeGateway(t, func(op string) []string {
		if op != "voice_join" {
			return nil
		}

		return []string{
			`{"t":"VOICE_STATE_UPDATE","d":{"user_id":"me","channel_id":"v1"}}`,
			`{"t":"VOICE_READY","d":{"channel_id":"v1","connection":{"kind":"pion"}}}`,
			`{"t":"VOICE_STATE_UPDATE","d":{"user_id":"other","channel_id":"v2"}}`,
			`{"t":"VOICE_STATE_UPDATE","d":{"user_id":"me","channel_id":"v2"}}`, // another window joined v2
		}
	})

	s := Join(Options{Server: url, Token: "x", ChannelID: "v1", Headless: true, ListenOnly: true})
	if msg := waitStatus(t, s, Ended); msg != msgMoved {
		t.Fatalf("error %q", msg)
	}
}

func TestSessionNoServer(t *testing.T) {
	s := Join(Options{Server: "http://127.0.0.1:1", Token: "x", ChannelID: "v1", Headless: true})
	if msg := waitStatus(t, s, Failed); msg != msgNoServer {
		t.Fatalf("error %q", msg)
	}
}
