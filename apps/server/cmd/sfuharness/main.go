// Command sfuharness exposes the built-in SFU over stdin/stdout, one JSON object per line, so that
// media clients (the Rust engine's tests) can be exercised against it without the rest of the server.
//
//	in:  {"user": "alice", "op": "join" | "leave" | "rtc_offer" | "rtc_answer" | "rtc_subscribe", "d": {...}}
//	out: {"user": "alice", "t": "RTC_ANSWER" | "RTC_OFFER" | "ERROR", "d": {...}}
//
// Everyone is in the same room with every permission.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"log"
	"os"
	"sync"

	"discordo/server/internal/media"
	"discordo/server/internal/media/pion"
)

func main() {
	sfu, err := pion.New(pion.Config{})
	if err != nil {
		log.Fatal(err)
	}
	var mu sync.Mutex
	out := json.NewEncoder(os.Stdout)
	reply := func(user, typ string, data any) {
		mu.Lock()
		defer mu.Unlock()
		_ = out.Encode(map[string]any{"user": user, "t": typ, "d": data})
	}

	ctx := context.Background()
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(nil, 1<<20)
	for in.Scan() {
		var msg struct {
			User string          `json:"user"`
			Op   string          `json:"op"`
			D    json.RawMessage `json:"d"`
		}
		if err := json.Unmarshal(in.Bytes(), &msg); err != nil {
			log.Fatal(err)
		}
		user := msg.User
		switch msg.Op {
		case "join":
			_, err = sfu.Join(ctx, "room", user, media.Grants{Speak: true, Stream: true},
				func(typ string, data any) { reply(user, typ, data) })
		case "leave":
			sfu.Leave("room", user)
		default:
			err = sfu.Signal(ctx, "room", user, msg.Op, msg.D)
		}
		if err != nil {
			reply(user, "ERROR", err.Error())
		}
	}
}
