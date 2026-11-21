package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gonativeui/app"
)

func TestChatMentionQuery(t *testing.T) {
	for _, tc := range []struct {
		text  string
		caret int
		start int
		query string
		ok    bool
	}{
		{"@", 1, 0, "", true},
		{"oi @an", 6, 3, "an", true},
		{"oi @an tudo", 6, 3, "an", true}, // the caret, not the end
		{"oi @an tudo", 11, 0, "", false},
		{"email@x", 7, 0, "", false}, // needs a space before
		{"é @ç", len("é @ç"), len("é "), "ç", true},
		{"<@u1> ", 6, 0, "", false},
		{"a\n@b", 4, 2, "b", true},
	} {
		start, query, ok := chatMentionQuery(tc.text, tc.caret)
		if ok != tc.ok || ok && (start != tc.start || query != tc.query) {
			t.Errorf("%q at %d: %d %q %v", tc.text, tc.caret, start, query, ok)
		}
	}
}

func TestChatMatches(t *testing.T) {
	people := []chatPerson{{ana, "Ana"}, {beto, "beto"}, {caio, "Caio"}}
	if got := chatMatches(people, "A"); len(got) != 2 || got[0].user.ID != "u1" || got[1].user.ID != "u3" {
		t.Fatalf("A: %+v", got)
	}

	many := make([]chatPerson, 20)
	if got := chatMatches(many, ""); len(got) != 8 {
		t.Fatalf("at most 8: %d", len(got))
	}
}

func TestTypingText(t *testing.T) {
	for n, want := range map[int]string{
		0: "", 1: "Ana está digitando…", 2: "Ana e Beto estão digitando…", 3: "Várias pessoas estão digitando…",
	} {
		if got := typingText([]string{"Ana", "Beto", "Caio"}[:n]); got != want {
			t.Errorf("%d: %q", n, got)
		}
	}
}

// TestChatRender draws a chat with files in the composer, someone typing and a message being edited.
func TestChatRender(t *testing.T) {
	at := time.Date(2026, 11, 16, 21, 5, 0, 0, time.Local)
	d := newDiscordo("http://localhost:0")
	d.st, d.phase, d.ready = ready(t), phaseOnline, true
	d.st.Guilds[0].Channels[0].Permissions |= permAttach

	d.st.AddMessages("c1", true,
		Message{ID: "0001", ChannelID: "c1", Author: beto, Content: "Bom dia <@u1>!", CreatedAt: at},
		Message{ID: "0002", ChannelID: "c1", Author: ana, Content: "Texto para editar", CreatedAt: at.Add(time.Minute)},
		Message{ID: "0003", ChannelID: "c1", Author: caio, Content: "Vídeo", CreatedAt: at.Add(20 * time.Minute),
			Attachments: []Attachment{{ID: "a1", Filename: "clipe.mp4", ContentType: "video/mp4", Size: 9_000_000}}},
	)
	d.st.Typing["c1"] = map[string]time.Time{"u2": time.Now().Add(time.Minute), "u1": time.Now().Add(time.Minute)}

	o, err := app.NewOffscreen(config(), 1280, 800, 1, d)
	if err != nil {
		t.Skip("no offscreen renderer here:", err)
	}
	defer o.Close()

	d.open("g1", "c1")
	cx := d.cx()
	cx.uploads = []chatUpload{
		{key: 1, name: "relatorio.pdf", size: 2_400_000, att: &Attachment{ID: "a9"}},
		{key: 2, name: "foto.png", size: 300_000},
		{key: 3, name: "enorme.zip", size: 30 << 20, err: "Maior que 25 MB"},
	}
	cx.nextKey = 3

	img, _ := o.Frame()
	d.chatStartEdit(&d.st.Logs["c1"].List[1])
	for range 6 {
		img, _ = o.Frame()
	}

	if ids, uploading := d.chatReady(); len(ids) != 1 || !uploading || d.chatCanSend() {
		t.Fatalf("ready %v uploading %v", ids, uploading)
	}

	if dir := os.Getenv("DISCORDO_SHOTS"); dir != "" {
		f, err := os.Create(filepath.Join(dir, "chat.png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()

		png.Encode(f, img)
	}
}
