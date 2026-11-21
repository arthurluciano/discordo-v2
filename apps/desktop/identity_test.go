package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gonativeui/app"
	"gonativeui/ui"
)

func TestMemberSince(t *testing.T) {
	got := memberSince(time.Date(2025, 5, 3, 12, 0, 0, 0, time.Local))
	if got != "3 de mai. de 2025" {
		t.Fatalf("memberSince = %q", got)
	}
}

func TestClampRunes(t *testing.T) {
	var b ui.TextBuffer
	b.SetString("ação!")
	clampRunes(&b, 3)

	if b.String() != "açã" {
		t.Fatalf("clampRunes = %q", b.String())
	}

	b.SetString("ok")
	clampRunes(&b, 3)

	if b.String() != "ok" {
		t.Fatalf("clampRunes cut a short text: %q", b.String())
	}
}

func TestBannerFill(t *testing.T) {
	if tl, br := bannerFill(0xeb459e, 0); tl != 0xeb459eff || br != tl {
		t.Fatalf("a colour banner is flat: %08x %08x", tl, br)
	}

	if tl, br := bannerFill(0, surfaceRail); tl == br {
		t.Fatal("the default banner is a gradient")
	}
}

// TestRenderIdentity draws the profile card and the settings; DISCORDO_SHOTS=dir saves them.
func TestRenderIdentity(t *testing.T) {
	shots := []struct {
		name string
		open func(d *Discordo)
	}{
		{"profile", func(d *Discordo) {
			idn.profileOpen, idn.profileID = true, "u2"
			idn.profiles = map[string]Profile{"u2": {User: beto, Bio: "Faço deploys às sextas.", Pronouns: "ele/dele", CreatedAt: time.Date(2024, 3, 9, 0, 0, 0, 0, time.Local), MutualGuildIDs: []string{"g1"}}}
		}},
		{"settings", func(d *Discordo) {
			idn.settingsOpen, idn.section, idn.formReady = true, userProfileSection, false
			idn.profiles = map[string]Profile{"u1": {User: ana, Bio: "Oi!", BannerColor: 0xeb459e, CreatedAt: time.Date(2024, 3, 9, 0, 0, 0, 0, time.Local)}}
		}},
		{"account", func(d *Discordo) {
			idn.settingsOpen, idn.section = true, userAccountSection
		}},
		{"group-picker", func(d *Discordo) { d.openFriendPicker("") }},
	}

	for _, s := range shots {
		t.Run(s.name, func(t *testing.T) {
			d := newDiscordo("http://localhost:0")
			d.st, d.phase, d.ready = ready(t), phaseOnline, true
			d.open("", "d2")

			o, err := app.NewOffscreen(config(), 1280, 800, 1, d)
			if err != nil {
				t.Skip("no offscreen renderer here:", err)
			}
			defer o.Close()

			idn.profileOpen, idn.settingsOpen, soc.picker = false, false, false // the dialogs are package state
			s.open(d)

			img, _ := o.Frame()
			for range 8 {
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
