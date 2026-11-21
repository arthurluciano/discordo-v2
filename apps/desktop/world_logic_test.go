package main

import (
	"bytes"
	"image"
	"image/png"
	"math"
	"testing"
)

func TestWorldStep(t *testing.T) {
	bounds := WorldPoint{1000, 1000}
	walls := []WorldRect{{X: 500, Y: 0, W: 16, H: 600}}
	near := func(a, b float32) bool { return math.Abs(float64(a-b)) < 0.01 }

	// A diagonal is as fast as a straight line.
	p := step(WorldPoint{100, 100}, WorldPoint{1, 1}, 0.05, walls, bounds)
	if d := math.Hypot(float64(p.X-100), float64(p.Y-100)); !near(float32(d), worldSpeed*0.05) {
		t.Fatalf("diagonal walked %v", d)
	}

	// Into a wall at an angle: x stops at it, y slides along.
	p = WorldPoint{470, 100}
	for range 20 {
		p = step(p, WorldPoint{1, 1}, 0.05, walls, bounds)
	}

	if p.X > 500-worldRadius || p.X < 465 || p.Y < 250 {
		t.Fatalf("wall: %+v", p)
	}

	// The map's edge holds the whole avatar in.
	p = WorldPoint{30, 30}
	for range 20 {
		p = step(p, WorldPoint{-1, -0.2}, 0.05, nil, bounds)
	}

	if p.X < worldRadius || p.Y < worldRadius || p.X > 30 {
		t.Fatalf("edge: %+v", p)
	}

	if q := step(p, WorldPoint{}, 1, nil, bounds); q != p {
		t.Fatalf("no direction moved: %+v", q)
	}
}

func TestWorldGain(t *testing.T) {
	at := func(x float32, zone string) VoiceState { return VoiceState{X: x, Zone: zone} }

	for _, c := range []struct {
		me, other VoiceState
		want      float32
	}{
		{at(0, ""), at(100, ""), 1},
		{at(0, ""), at(120, ""), 1},
		{at(0, ""), at(260, ""), 0.5},
		{at(0, ""), at(400, ""), 0},
		{at(0, ""), at(900, ""), 0},
		{at(0, "cinema"), at(900, "cinema"), 1},
		{at(0, "cinema"), at(10, ""), 0},
		{at(0, ""), at(10, "lol"), 0},
		{at(0, "cs2"), at(10, "lol"), 0},
	} {
		if got := gain(c.me, c.other); math.Abs(float64(got-c.want)) > 0.001 {
			t.Errorf("gain(%+v, %+v) = %v, want %v", c.me, c.other, got, c.want)
		}
	}
}

func TestZoneAt(t *testing.T) {
	m := &WorldMap{Zones: []WorldZone{{ID: "a", WorldRect: WorldRect{X: 100, Y: 100, W: 200, H: 100}}}}

	if z := zoneAt(m, 100, 100); z == nil || z.ID != "a" {
		t.Fatal("the top-left corner is inside")
	}

	if zoneAt(m, 300, 150) != nil || zoneAt(m, 50, 150) != nil {
		t.Fatal("the right edge and the plaza are outside")
	}
}

func TestSprite(t *testing.T) {
	var b bytes.Buffer
	png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 3*spriteW, 4*spriteH)))

	frames := cutSprite(b.Bytes())
	if len(frames) != 12 {
		t.Fatalf("%d frames", len(frames))
	}

	if cfg, err := png.DecodeConfig(bytes.NewReader(frames[11])); err != nil || cfg.Width != spriteW || cfg.Height != spriteH {
		t.Errorf("frame: %+v %v", cfg, err)
	}

	if cutSprite([]byte("no picture")) != nil {
		t.Error("cut something that is not a sheet")
	}

	// Walking left steps on the second row; standing still keeps facing there.
	if f, face := spriteFrame(0, -3, 1, true, 150); f != 5 || face != 1 {
		t.Errorf("walking left: frame %d facing %d", f, face)
	}

	if f, _ := spriteFrame(1, 0, 0, false, 150); f != 3 {
		t.Errorf("standing: frame %d", f)
	}
}
