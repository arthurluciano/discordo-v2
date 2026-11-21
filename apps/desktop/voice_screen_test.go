package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"discordo/desktop/voice"

	"gonativeui/app"
)

// fakeDesktop is a picture of a desktop: a dark background, a lighter window with a title bar, a blue tint.
func fakeDesktop(t *testing.T, w, h int) voice.Picture {
	p, err := voice.NewPicture(w, h, func(y, uv []byte, ys, uvs int) {
		for r := range h {
			for c := range w {
				v := byte(40 + 30*r/h)
				if c > w/6 && c < w*5/6 && r > h/6 && r < h*5/6 {
					v = 225
					if r < h/6+h/20 {
						v = 120
					}
				}

				y[r*ys+c] = v
			}
		}

		for i := 0; i+1 < len(uv); i += 2 {
			uv[i], uv[i+1] = 150, 115
		}
	})
	if err != nil {
		t.Skip("no pictures here:", err)
	}

	return p
}

// TestVoiceScreenRender draws the screens of a call offscreen: watched and not, focused, over the window, and the
// share dialog with a preview; DISCORDO_SHOTS=dir saves them.
func TestVoiceScreenRender(t *testing.T) {
	shots := []struct {
		name         string
		pinned       string
		full, dialog bool
	}{
		{name: "voice-screens"},
		{name: "voice-screen-focus", pinned: "u2"},
		{name: "voice-screen-full", pinned: "u2", full: true},
		{name: "voice-share", dialog: true},
	}

	pic := fakeDesktop(t, 1920, 1080)
	defer pic.Release()

	frame := voiceFrame
	voiceFrame = func(userID, track string) voice.Picture {
		if userID == "u3" {
			return 0 // not watched: no picture
		}

		return pic.Retain()
	}
	defer func() { voiceFrame = frame }()

	for _, s := range shots {
		t.Run(s.name, func(t *testing.T) {
			d := newDiscordo("http://localhost:0")
			d.st, d.phase, d.ready = ready(t), phaseOnline, true
			d.st.Voice["u1"] = VoiceState{UserID: "u1", ChannelID: "v1", Tracks: []string{"mic"}}
			d.st.Voice["u2"] = VoiceState{UserID: "u2", ChannelID: "v1", Muted: true, Tracks: []string{"mic", "screen"}}
			d.st.Voice["u3"] = VoiceState{UserID: "u3", ChannelID: "v1", Tracks: []string{"mic", "screen"}}
			d.st.Guild("g1").Channel("v1").Permissions = permConnect | permSpeak | permStream

			o, err := app.NewOffscreen(config(), 1280, 800, 1, d)
			if err != nil {
				t.Skip("no offscreen renderer here:", err)
			}
			defer o.Close()

			d.open("g1", "v1")

			// In a call still asking for the microphone: no session, nothing on the network.
			vc.channel, vc.gen, vc.starting = "v1", d.gen, true
			vc.scr = voiceScreens{watching: map[string]bool{"u2": true}, pinned: s.pinned, full: s.full, dialog: s.dialog}
			defer func() { vc.channel, vc.starting = "", false; voiceScreenReset() }()

			img, _ := o.Frame()
			for range 6 {
				img, _ = o.Frame()
			}

			if vc.channel != "v1" || vc.scr.full != s.full || vc.scr.dialog != s.dialog {
				t.Fatalf("drawing changed the call: channel %q full %v dialog %v", vc.channel, vc.scr.full, vc.scr.dialog)
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
