package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gonativeui/app"
	"gonativeui/ui"
)

// resetTheme puts the Discordo skin back and forgets the store, for the tests that follow.
func resetTheme(t *testing.T) {
	t.Cleanup(func() {
		if themeStore != nil {
			themeStore.Close()
		}

		themeStore, skinNow, themeStill = nil, 0, false
		skins[0].p.apply()
	})
}

func TestThemePrefs(t *testing.T) {
	resetTheme(t)
	path := filepath.Join(t.TempDir(), "theme.json")

	openThemePrefs(path)
	if skinNow != 0 || themeStill || blurple != 0x5865f2ff {
		t.Fatalf("a new file starts on Discordo: skin %d, still %v, blurple %08x", skinNow, themeStill, blurple)
	}

	setSkin(skinIndex("forest"))
	setStill(true)
	if blurple != 0x2f9e6eff || surfaceSidebar != 0x15231cff || avatarColors[0] != blurple {
		t.Fatalf("forest not painted: blurple %08x, sidebar %08x", blurple, surfaceSidebar)
	}

	if err := themeStore.Close(); err != nil {
		t.Fatal(err)
	}

	skins[0].p.apply()
	openThemePrefs(path)
	if skins[skinNow].id != "forest" || !themeStill || blurple != 0x2f9e6eff {
		t.Fatalf("not kept: skin %q, still %v", skins[skinNow].id, themeStill)
	}

	if skinIndex("glass") != 0 || skinIndex("") != 0 {
		t.Fatal("an unknown skin must fall back to Discordo")
	}
}

// appearanceOnly draws the "Aparência" section alone, as the settings dialog's content.
type appearanceOnly struct{ *Discordo }

func (a appearanceOnly) Build(c *ui.Context) {
	k := a.k
	k.Begin(c)
	a.fonts()
	w, h := c.Size()
	a.begin("dialog", box{axis: ui.AxisY, w: ui.Px(float32(w)), h: ui.Px(float32(h)), bg: surfaceSidebar, l: 24, r: 24, t: 24, b: 24})
	a.appearanceSection()
	c.Pop()
	k.End()
}

// TestThemeShots draws the workspace in every skin, and the settings section; DISCORDO_SHOTS=dir saves them.
func TestThemeShots(t *testing.T) {
	resetTheme(t)
	dragon = bakeDragon() // at once, not on a goroutine

	for i, s := range skins {
		t.Run(s.id, func(t *testing.T) {
			setSkin(i)
			d := newDiscordo("http://localhost:0")
			d.st, d.phase, d.ready = ready(t), phaseOnline, true

			o, err := app.NewOffscreen(config(), 1440, 860, 1, d)
			if err != nil {
				t.Skip("no offscreen renderer here:", err)
			}
			defer o.Close()

			d.open("g1", "c1")

			img, _ := o.Frame()
			for range 30 { // the dragon's images decode on the cache's workers
				img, _ = o.Frame()
				time.Sleep(5 * time.Millisecond)
			}

			save(t, s.id, img)
		})
	}

	setSkin(skinIndex("dragon"))
	d := newDiscordo("http://localhost:0")
	o, err := app.NewOffscreen(config(), 720, 720, 1, appearanceOnly{d})
	if err != nil {
		t.Skip("no offscreen renderer here:", err)
	}
	defer o.Close()

	img, _ := o.Frame()
	for range 4 {
		img, _ = o.Frame()
	}

	save(t, "appearance", img)
}

func save(t *testing.T, name string, m *image.RGBA) {
	dir := os.Getenv("DISCORDO_SHOTS")
	if dir == "" {
		return
	}

	f, err := os.Create(filepath.Join(dir, "theme-"+name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if err := png.Encode(f, m); err != nil {
		t.Fatal(err)
	}
}
