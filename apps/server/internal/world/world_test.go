package world

import (
	"bytes"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestMap(t *testing.T) {
	m := Default

	for _, z := range m.Zones {
		if got := m.ZoneAt(z.Spot.X, z.Spot.Y); got != z.ID {
			t.Errorf("ZoneAt(%s's spot) = %q", z.ID, got)
		}
	}

	if m.ZoneAt(m.Spawn.X, m.Spawn.Y) != "" || m.ZoneAt(750, 800) != "" {
		t.Error("the plaza is no room")
	}

	nan := float32(math.NaN())
	if m.Contains(-1, 0) || m.Contains(0, m.Height+1) || m.Contains(nan, 0) || !m.Contains(m.Spawn.X, m.Spawn.Y) {
		t.Error("Contains")
	}

	// The art is drawn on a grid: a measure off it would blur or leave a gap between picture and collision.
	for _, w := range m.Walls {
		for _, v := range []float32{w.X, w.Y, w.W, w.H} {
			if v != float32(int(v)) || int(v)%Pixel != 0 {
				t.Fatalf("wall off the pixel grid: %+v", w)
			}
		}
	}
}

// Furniture must never close a door: every room's spot is reached on foot from the spawn.
func TestEveryRoomIsReachable(t *testing.T) {
	m := Default

	const cell, radius = 12, 20
	cols, rows := int(m.Width)/cell, int(m.Height)/cell

	free := func(cx, cy int) bool {
		x, y := float32(cx*cell+cell/2), float32(cy*cell+cell/2)
		if x < radius || y < radius || x > m.Width-radius || y > m.Height-radius {
			return false
		}

		for _, w := range m.Walls {
			if x+radius > w.X && x-radius < w.X+w.W && y+radius > w.Y && y-radius < w.Y+w.H {
				return false
			}
		}

		return true
	}

	seen := map[[2]int]bool{}
	queue := [][2]int{{int(m.Spawn.X) / cell, int(m.Spawn.Y) / cell}}
	if !free(queue[0][0], queue[0][1]) {
		t.Fatal("the spawn is inside something")
	}

	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]

		for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			n := [2]int{p[0] + d[0], p[1] + d[1]}
			if n[0] < 0 || n[1] < 0 || n[0] >= cols || n[1] >= rows || seen[n] || !free(n[0], n[1]) {
				continue
			}

			seen[n] = true
			queue = append(queue, n)
		}
	}

	for _, z := range m.Zones {
		if !seen[[2]int{int(z.Spot.X) / cell, int(z.Spot.Y) / cell}] {
			t.Errorf("%s: its spot cannot be reached from the spawn", z.ID)
		}
	}
}

// WORLD_ART=dir saves the pictures to look at.
func TestArt(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(MapPNG()))
	if err != nil {
		t.Fatal(err)
	}

	if b := img.Bounds(); b.Dx() != int(Default.Width) || b.Dy() != int(Default.Height) {
		t.Fatalf("map picture is %v, the map %vx%v", b, Default.Width, Default.Height)
	}

	a, b := SpritePNG("u1"), SpritePNG("u1")
	sheet, err := png.Decode(bytes.NewReader(a))
	if err != nil || !bytes.Equal(a, b) {
		t.Fatal("a sprite must decode and be the same for the same seed", err)
	}

	if s := sheet.Bounds(); s.Dx() != SpriteW*3 || s.Dy() != SpriteH*4 {
		t.Fatalf("sheet is %v", s)
	}

	if dir := os.Getenv("WORLD_ART"); dir != "" {
		os.WriteFile(filepath.Join(dir, "map.png"), MapPNG(), 0o644)
		for _, seed := range []string{"u1", "u2", "u3", "ana", "beto", "caio", "x", "y"} {
			os.WriteFile(filepath.Join(dir, "sprite-"+seed+".png"), SpritePNG(seed), 0o644)
		}
	}
}
