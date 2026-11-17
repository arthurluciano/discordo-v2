package world

import (
	"math"
	"testing"
)

func TestMap(t *testing.T) {
	m := Default

	for _, c := range []struct {
		x, y float32
		zone string
	}{{1200, 800, ""}, {400, 340, "valorant"}, {1200, 340, "lol"}, {2000, 340, "cs2"}, {1200, 1110, "cinema"}, {750, 800, ""}} {
		if got := m.ZoneAt(c.x, c.y); got != c.zone {
			t.Errorf("ZoneAt(%v, %v) = %q, want %q", c.x, c.y, got, c.zone)
		}
	}

	nan := float32(math.NaN())
	if m.Contains(-1, 0) || m.Contains(0, m.Height+1) || m.Contains(nan, 0) || !m.Contains(m.Spawn.X, m.Spawn.Y) {
		t.Error("Contains")
	}

	// Every room: four walls, one of them split by the door; the spot is inside and off the walls.
	if len(m.Walls) != 5*len(m.Zones) {
		t.Fatalf("%d walls for %d zones", len(m.Walls), len(m.Zones))
	}

	for _, z := range m.Zones {
		if m.ZoneAt(z.Spot.X, z.Spot.Y) != z.ID {
			t.Errorf("%s: spot outside", z.ID)
		}

		for _, w := range m.Walls {
			if w.has(z.Spot.X, z.Spot.Y) {
				t.Errorf("%s: spot in a wall", z.ID)
			}
		}
	}
}
