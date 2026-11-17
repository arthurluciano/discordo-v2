package main

import (
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gonativeui/app"
)

func event(t *testing.T, kind string, d any) Event {
	t.Helper()

	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}

	return Event{T: kind, D: b}
}

var (
	ana  = User{ID: "u1", Username: "ana", DisplayName: "Ana"}
	beto = User{ID: "u2", Username: "beto"}
	caio = User{ID: "u3", Username: "caio", DisplayName: "Caio"}
)

func ready(t *testing.T) *State {
	s := NewState()

	err := s.Apply(event(t, "READY", map[string]any{
		"user": ana,
		"guilds": []Guild{{
			ID: "g1", Name: "Oficina Discordo", OwnerID: "u1", Permissions: permManage,
			Roles:   []Role{{ID: "r1", Color: 0xeb459e, Position: 1}},
			Members: []Member{{User: ana, Online: true}, {User: beto, Roles: []string{"r1"}, Online: true}, {User: caio}},
			Channels: []Channel{
				{ID: "c1", Name: "geral", Topic: "Conversa livre do time", Permissions: permSend | permReact},
				{ID: "c2", Name: "avisos"}, {ID: "v1", Type: ChannelVoice, Name: "Sala de voz"},
			},
		}, {ID: "g2", Name: "Jogos de sexta"}},
		"dms":           []DM{{ID: "d1", Type: ChannelDM, Recipients: []User{ana, beto}}, {ID: "d2", Type: ChannelGroup, Name: "Viagem", Recipients: []User{ana, beto, caio}}},
		"relationships": []Relationship{{User: beto, Status: 1, Online: true}, {User: caio, Status: 2}},
		"voice_states":  []VoiceState{{UserID: "u2", ChannelID: "v1", Muted: true}},
	}))
	if err != nil {
		t.Fatal(err)
	}

	return s
}

func TestApply(t *testing.T) {
	s := ready(t)
	s.AddMessages("c1", true) // the channel was opened

	// Out of order and repeated: kept once each, in ID order, with mentions by name.
	for _, m := range []Message{
		{ID: "0002", ChannelID: "c1", Author: beto, Content: "oi <@u1>"},
		{ID: "0001", ChannelID: "c1", Author: ana, Content: "bom dia"},
		{ID: "0002", ChannelID: "c1", Author: beto, Content: "oi <@u1>, tudo bem? <@zz>"},
	} {
		if err := s.Apply(event(t, "MESSAGE_CREATE", m)); err != nil {
			t.Fatal(err)
		}
	}

	log := s.Logs["c1"].List
	if len(log) != 2 || log[0].ID != "0001" || string(log[1].Body) != "oi @Ana, tudo bem? @usuário desconhecido" {
		t.Fatalf("log: %+v", log)
	}

	if sp := log[1].Spans; len(sp) != 2 || string(log[1].Body[sp[0].Off:sp[0].Off+sp[0].Len]) != "@Ana" {
		t.Fatalf("spans: %+v", sp)
	}

	// A channel never opened keeps no log: it loads whole when opened.
	s.Apply(event(t, "MESSAGE_CREATE", Message{ID: "0009", ChannelID: "c2", Author: ana}))
	if s.Logs["c2"] != nil {
		t.Fatal("unopened channel got a log")
	}

	s.Apply(event(t, "REACTION_ADD", map[string]string{"channel_id": "c1", "message_id": "0001", "user_id": "u2", "emoji": "🔥"}))
	s.Apply(event(t, "REACTION_ADD", map[string]string{"channel_id": "c1", "message_id": "0001", "user_id": "u2", "emoji": "🔥"}))
	if r := s.Logs["c1"].List[0].Reactions; len(r) != 1 || len(r[0].Users) != 1 {
		t.Fatalf("reactions: %+v", r)
	}

	s.Apply(event(t, "REACTION_REMOVE", map[string]string{"channel_id": "c1", "message_id": "0001", "user_id": "u2", "emoji": "🔥"}))
	if r := s.Logs["c1"].List[0].Reactions; len(r) != 0 {
		t.Fatalf("reaction not removed: %+v", r)
	}

	s.Apply(event(t, "MESSAGE_DELETE", map[string]string{"id": "0001", "channel_id": "c1"}))
	if len(s.Logs["c1"].List) != 1 {
		t.Fatalf("after delete: %+v", s.Logs["c1"].List)
	}

	s.Apply(event(t, "PRESENCE_UPDATE", map[string]any{"user_id": "u3", "online": true}))
	if !s.Online("u3") || !s.Guild("g1").Members[2].Online {
		t.Fatal("presence")
	}

	if colors := s.Guild("g1").RoleColors(); colors["u2"] != 0xeb459e || len(colors) != 1 {
		t.Fatalf("role colors: %v", colors)
	}

	if s.DMName(s.DMs[0]) != "beto" || len(s.Callers("v1")) != 1 {
		t.Fatal("dm name or callers")
	}

	s.Apply(event(t, "VOICE_STATE_UPDATE", VoiceState{UserID: "u2"}))
	s.Apply(event(t, "GUILD_DELETE", map[string]string{"id": "g1"}))
	if len(s.Guilds) != 1 || len(s.Callers("v1")) != 0 {
		t.Fatal("guild or voice state not removed")
	}
}

func TestInitials(t *testing.T) {
	for in, want := range map[string]string{"Oficina Discordo": "OD", "jogos": "J", "  ": "?", "a b c": "AB", "ébano": "É"} {
		if got := initialsOf(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

// TestRender draws the screens offscreen; DISCORDO_SHOTS=dir saves them to compare with the web app.
func TestRender(t *testing.T) {
	at := time.Date(2026, 11, 16, 21, 5, 0, 0, time.Local)
	msg := func(id string, u User, text string, after time.Duration) Message {
		return Message{ID: id, ChannelID: "c1", Author: u, Content: text, CreatedAt: at.Add(after)}
	}

	shots := []struct {
		name           string
		w, h           int
		guild, channel string
		phase          phase
	}{
		{"channel", 1440, 860, "g1", "c1", phaseOnline},
		{"dm", 1280, 800, "", "d1", phaseOnline},
		{"friends", 1280, 800, "", "", phaseOnline},
		{"login", 1440, 860, "", "", phaseSignedOut},
	}

	for _, s := range shots {
		t.Run(s.name, func(t *testing.T) {
			d := newDiscordo("http://localhost:0")
			d.st, d.phase, d.ready = ready(t), s.phase, true

			first := msg("0001", beto, "Bom dia, pessoal! O deploy de ontem passou.", 0)
			first.Reactions = []Reaction{{Emoji: "🔥", Users: []string{"u1", "u2"}}, {Emoji: "👍", Users: []string{"u2"}}}
			d.st.AddMessages("c1", true,
				first,
				msg("0002", beto, "Alguém viu o tema novo?", time.Minute),
				msg("0003", ana, "Vi sim <@u2>, ficou ótimo. Uma mensagem bem mais longa para ver a quebra de linha funcionando no app nativo, sem cortar no fim.", 3*time.Minute),
				Message{ID: "0004", ChannelID: "c1", Author: caio, Content: "<@u1> olha o relatório", CreatedAt: at.Add(20 * time.Minute), Mentions: []string{"u1"},
					Attachments: []Attachment{{ID: "a1", Filename: "relatorio.pdf", ContentType: "application/pdf", Size: 2_400_000}}},
			)
			d.st.AddMessages("d1", true, Message{ID: "0005", ChannelID: "d1", Author: beto, Content: "E aí, vamos hoje?", CreatedAt: at})

			o, err := app.NewOffscreen(config(), s.w, s.h, 1, d)
			if err != nil {
				t.Skip("no offscreen renderer here:", err)
			}
			defer o.Close()

			d.open(s.guild, s.channel)

			img, _ := o.Frame()
			for range 6 {
				img, _ = o.Frame()
			}

			if dir := os.Getenv("DISCORDO_SHOTS"); dir != "" {
				f, err := os.Create(filepath.Join(dir, s.name+".png"))
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()

				png.Encode(f, img)
			}
		})
	}
}
