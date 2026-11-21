package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"gonativeui/app"
)

// TestVoiceRender draws a voice channel's stage, the call's dock and the devices dialog offscreen;
// DISCORDO_SHOTS=dir saves them.
func TestVoiceRender(t *testing.T) {
	shots := []struct {
		name             string
		inCall, settings bool
		muted, deafened  bool
	}{
		{name: "voice"},
		{name: "voice-call", inCall: true, muted: true},
		{name: "voice-settings", inCall: true, settings: true, deafened: true},
	}

	for _, s := range shots {
		t.Run(s.name, func(t *testing.T) {
			d := newDiscordo("http://localhost:0")
			d.st, d.phase, d.ready = ready(t), phaseOnline, true

			o, err := app.NewOffscreen(config(), 1280, 800, 1, d)
			if err != nil {
				t.Skip("no offscreen renderer here:", err)
			}
			defer o.Close()

			d.open("g1", "v1")

			// In a call that is still asking for the microphone: no session, nothing on the network.
			vc.channel, vc.gen, vc.starting, vc.settings = "", 0, false, false
			vc.muted, vc.deafened = s.muted, s.deafened
			if s.inCall {
				vc.channel, vc.gen, vc.starting, vc.settings = "v1", d.gen, true, s.settings
			}
			defer func() { vc.channel, vc.starting, vc.settings, vc.muted, vc.deafened = "", false, false, false, false }()

			img, _ := o.Frame()
			for range 6 {
				img, _ = o.Frame()
			}

			if s.inCall && vc.channel != "v1" {
				t.Fatal("drawing the call hung up")
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
