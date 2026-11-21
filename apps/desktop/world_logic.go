package main

// The world's rules, without the window: walking with collisions, who hears whom, which room a point is in.

import (
	"bytes"
	"image"
	"image/png"
	"math"
)

const (
	worldRadius = 20  // an avatar's, in map units
	worldSpeed  = 260 // map units a second
	worldNear   = 120 // in the plaza: full volume up to here
	worldFar    = 400 // and silence from here

	// A character's sheet (GET /world/sprites/{id}): 3 columns (standing, two steps) of 4 rows (facing down,
	// left, right, up) of frames this size in map units, the feet this far above a frame's bottom edge.
	spriteW, spriteH = 60, 78
	spriteFeet       = 6
)

// cutSprite cuts a character's sheet into its 12 frames (row × 3 + column), each a PNG; nil when it is not one.
func cutSprite(sheet []byte) [][]byte {
	src, err := png.Decode(bytes.NewReader(sheet))
	if err != nil || src.Bounds().Dx() < 3*spriteW || src.Bounds().Dy() < 4*spriteH {
		return nil
	}

	sub, ok := src.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return nil
	}

	frames := make([][]byte, 12)
	for i := range frames {
		min := src.Bounds().Min.Add(image.Pt(i%3*spriteW, i/3*spriteH))

		var b bytes.Buffer
		if png.Encode(&b, sub.SubImage(image.Rectangle{Min: min, Max: min.Add(image.Pt(spriteW, spriteH))})) != nil {
			return nil
		}

		frames[i] = b.Bytes()
	}

	return frames
}

// spriteFrame is the frame for a character that moved by (dx, dy): the row it faces (kept when it did not move)
// and, while walking, the two steps in turn every 150 ms.
func spriteFrame(face int, dx, dy float32, walking bool, ms int64) (frame, facing int) {
	if walking {
		switch {
		case dx*dx > dy*dy && dx < 0:
			face = 1
		case dx*dx > dy*dy:
			face = 2
		case dy < 0:
			face = 3
		default:
			face = 0
		}

		return face*3 + 1 + int(ms/150%2), face
	}

	return face * 3, face
}

// worldBlocked says whether an avatar centred on p overlaps a wall or leaves the map.
func worldBlocked(p WorldPoint, walls []WorldRect, bounds WorldPoint) bool {
	if p.X < worldRadius || p.Y < worldRadius || p.X > bounds.X-worldRadius || p.Y > bounds.Y-worldRadius {
		return true
	}

	for _, w := range walls {
		dx := p.X - max(w.X, min(p.X, w.X+w.W))
		dy := p.Y - max(w.Y, min(p.Y, w.Y+w.H))
		if dx*dx+dy*dy < worldRadius*worldRadius {
			return true
		}
	}

	return false
}

// step walks from pos along dir (any length) for dt seconds. Each axis moves on its own, so a wall met at an
// angle is slid along. Callers keep dt small enough not to jump a wall.
func step(pos, dir WorldPoint, dt float32, walls []WorldRect, bounds WorldPoint) WorldPoint {
	n := float32(math.Hypot(float64(dir.X), float64(dir.Y)))
	if n == 0 {
		return pos
	}

	dist := worldSpeed * dt / n

	if p := (WorldPoint{pos.X + dir.X*dist, pos.Y}); !worldBlocked(p, walls, bounds) {
		pos = p
	}

	if p := (WorldPoint{pos.X, pos.Y + dir.Y*dist}); !worldBlocked(p, walls, bounds) {
		pos = p
	}

	return pos
}

// gain is how loud me hears other, 0 to 1: rooms are closed, the plaza fades with the distance.
func gain(me, other VoiceState) float32 {
	if me.Zone != "" || other.Zone != "" {
		if me.Zone == other.Zone {
			return 1
		}

		return 0
	}

	d := float32(math.Hypot(float64(me.X-other.X), float64(me.Y-other.Y)))

	return max(0, min(1, (worldFar-d)/(worldFar-worldNear)))
}

// zoneAt is the room at a point (nil in the plaza), by the server's rule.
func zoneAt(m *WorldMap, x, y float32) *WorldZone {
	for i := range m.Zones {
		if z := &m.Zones[i]; x >= z.X && x < z.X+z.W && y >= z.Y && y < z.Y+z.H {
			return z
		}
	}

	return nil
}
