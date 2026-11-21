// Package world is the map every guild's world shares: a plaza, the game rooms and the cinema.
// The server knows where the map ends and which room a point is in, and paints it (art.go) and the characters
// (sprite.go) as pixel art; clients show those pictures and collide with Walls.
package world

import (
	_ "embed"
	"encoding/json"
)

type Point struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
}

type Rect struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
	W float32 `json:"w"`
	H float32 `json:"h"`
}

func (r Rect) has(x, y float32) bool { return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H }

// Zone is a room: everyone inside hears each other and nobody outside does.
type Zone struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`   // game, cinema
	Status string `json:"status"` // what the guild sees of who is inside: "Jogando Valorant"
	Color  uint32 `json:"color"`  // 0xRRGGBB
	Rect
	Door   string `json:"door"`             // the wall with the gap: top, bottom
	Spot   Point  `json:"spot"`             // where "go to" lands
	Screen *Rect  `json:"screen,omitempty"` // the cinema's
}

type Map struct {
	Width  float32 `json:"width"`
	Height float32 `json:"height"`
	Spawn  Point   `json:"spawn"`
	Zones  []Zone  `json:"zones"`
	Walls  []Rect  `json:"walls"` // everything solid: the rooms' walls and the furniture. Derived, as the art is
}

// Art is drawn on a grid of Pixel world units, so every measure here is a multiple of it.
const (
	Pixel   = 3
	wall    = 18  // thickness of a side or bottom wall
	topWall = 48  // the top wall shows its face
	door    = 144 // gap
)

//go:embed map.json
var raw []byte

var Default = load()

func load() Map {
	var m Map
	if err := json.Unmarshal(raw, &m); err != nil {
		panic("world: map.json: " + err.Error())
	}

	for _, z := range m.Zones {
		side := (z.W - door) / 2
		top := []Rect{{z.X, z.Y, z.W, topWall}}
		bottom := []Rect{{z.X, z.Y + z.H - wall, z.W, wall}}
		gap := func(y, h float32) []Rect {
			return []Rect{{z.X, y, side, h}, {z.X + z.W - side, y, side, h}}
		}

		if z.Door == "top" {
			top = gap(z.Y, topWall)
		} else {
			bottom = gap(z.Y+z.H-wall, wall)
		}

		m.Walls = append(m.Walls, top...)
		m.Walls = append(m.Walls, bottom...)
		m.Walls = append(m.Walls, Rect{z.X, z.Y, wall, z.H}, Rect{z.X + z.W - wall, z.Y, wall, z.H})
	}

	for _, p := range m.furniture() {
		if f := solid[p.kind]; !f.Empty() {
			m.Walls = append(m.Walls, Rect{
				float32(p.x+f.Min.X) * Pixel, float32(p.y+f.Min.Y) * Pixel, float32(f.Dx()) * Pixel, float32(f.Dy()) * Pixel,
			})
		}
	}

	return m
}

// Contains is false outside the map, and for NaN.
func (m Map) Contains(x, y float32) bool {
	return x >= 0 && x <= m.Width && y >= 0 && y <= m.Height
}

// ZoneAt is the ID of the room at a point, or "" in the plaza.
func (m Map) ZoneAt(x, y float32) string {
	for _, z := range m.Zones {
		if z.has(x, y) {
			return z.ID
		}
	}

	return ""
}
