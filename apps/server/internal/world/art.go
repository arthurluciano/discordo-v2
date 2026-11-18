package world

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"sort"
	"sync"
)

// The map's picture: pixel art painted from the zones, one art pixel to Pixel world units.
// Everything is drawn with rectangles on a small canvas and enlarged at the end, so it stays crisp.

type canvas struct{ *image.RGBA }

func newCanvas(w, h int) canvas { return canvas{image.NewRGBA(image.Rect(0, 0, w, h))} }

func rgb(v uint32) color.RGBA { return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255} }

// mix is a with t of b (0..1).
func mix(a, b color.RGBA, t float32) color.RGBA {
	f := func(x, y uint8) uint8 { return uint8(float32(x) + (float32(y)-float32(x))*t) }
	return color.RGBA{f(a.R, b.R), f(a.G, b.G), f(a.B, b.B), 255}
}

func (c canvas) px(x, y int, col color.RGBA) {
	if image.Pt(x, y).In(c.Rect) {
		c.SetRGBA(x, y, col)
	}
}

func (c canvas) rect(x, y, w, h int, col color.RGBA) {
	for j := y; j < y+h; j++ {
		for i := x; i < x+w; i++ {
			c.px(i, j, col)
		}
	}
}

// shade darkens what is already there: shadows under furniture and walls.
func (c canvas) shade(x, y, w, h int, t float32) {
	for j := y; j < y+h; j++ {
		for i := x; i < x+w; i++ {
			if image.Pt(i, j).In(c.Rect) {
				c.SetRGBA(i, j, mix(c.RGBAAt(i, j), rgb(0x000000), t))
			}
		}
	}
}

// disc is a filled circle, flattened by squash (1 round, 0.5 an ellipse half as tall).
func (c canvas) disc(cx, cy, r int, squash float32, col color.RGBA) {
	for j := -r; j <= r; j++ {
		for i := -r; i <= r; i++ {
			y := float32(j) / squash
			if float32(i*i)+y*y <= float32(r*r)+float32(r)*0.8 {
				c.px(cx+i, cy+j, col)
			}
		}
	}
}

// box is a block seen from above and a little from the front: a top and, under it, a darker front face.
func (c canvas) box(x, y, w, h, face int, top color.RGBA) {
	c.rect(x, y, w, h-face, top)
	c.rect(x, y, w, 1, mix(top, rgb(0xffffff), 0.25))
	c.rect(x, y+h-face, w, face, mix(top, rgb(0x000000), 0.35))
	c.shade(x, y+h, w, 1, 0.25)
}

// noise is a fixed pseudo-random 0..99 for a cell, so the picture is the same on every run.
func noise(x, y int) int {
	n := uint32(x)*374761393 + uint32(y)*668265263
	n = (n ^ (n >> 13)) * 1274126177

	return int((n ^ (n >> 16)) % 100)
}

var ink = rgb(0x1b1c26)

// piece is one thing placed on the map, at art pixels.
type piece struct {
	kind   string
	x, y   int
	accent color.RGBA
}

// solid is the part of each kind you cannot walk through, relative to the piece. Chairs and seats are free: you sit.
var solid = map[string]image.Rectangle{
	"desk":     image.Rect(0, 2, 30, 18),
	"sofa":     image.Rect(0, 0, 44, 16),
	"table":    image.Rect(0, 0, 20, 11),
	"arcade":   image.Rect(0, 4, 14, 24),
	"vending":  image.Rect(0, 4, 16, 26),
	"plant":    image.Rect(1, 8, 11, 17),
	"tree":     image.Rect(8, 22, 20, 32),
	"fountain": image.Rect(0, 4, 40, 32),
	"bench":    image.Rect(0, 0, 24, 9),
	"lamp":     image.Rect(1, 14, 5, 18),
	"popcorn":  image.Rect(0, 6, 18, 26),
	"counter":  image.Rect(0, 2, 40, 18),
	"speaker":  image.Rect(0, 0, 12, 30),
	"screen":   image.Rect(-14, -8, 238, 132), // with the curtains
	"planter":  image.Rect(0, 2, 30, 12),
}

func art(v float32) int { return int(v) / Pixel }

// furniture places everything from the zones, so moving a room in map.json moves what is in it.
func (m Map) furniture() []piece {
	var out []piece
	add := func(kind string, x, y int, accent color.RGBA) { out = append(out, piece{kind, x, y, accent}) }
	none := color.RGBA{}

	for _, z := range m.Zones {
		x, y, w, h := art(z.X), art(z.Y), art(z.W), art(z.H)
		accent := rgb(z.Color)

		if z.Kind == "cinema" {
			s := z.Screen
			add("screen", art(s.X), art(s.Y), accent)
			add("speaker", art(s.X)-30, art(s.Y)+40, accent)
			add("speaker", art(s.X+s.W)+18, art(s.Y)+40, accent)

			for row := range 3 {
				for i := range 8 {
					add("seat", x+22+i*14, y+50+row*20, accent)
					add("seat", x+w-34-i*14, y+50+row*20, accent)
				}
			}

			add("popcorn", x+12, y+20, accent)
			add("counter", x+w-54, y+22, accent)
			add("plant", x+40, y+26, accent)
			add("plant", x+w-70, y+26, accent)

			continue
		}

		for i := range 4 {
			add("desk", x+12+i*48, y+22, accent)
			add("chair", x+18+i*48, y+43, accent)
		}

		for i := range 2 {
			add("desk", x+12+i*48, y+74, accent)
			add("chair", x+18+i*48, y+95, accent)
		}

		add("rug", x+116, y+68, accent)
		add("sofa", x+132, y+72, accent)
		add("table", x+144, y+96, accent)
		add("arcade", x+10, y+h-36, accent)
		add("vending", x+28, y+h-38, accent)
		add("plant", x+w-22, y+h-30, accent)
		add("plant", x+w-22, y+70, accent)
	}

	// The plaza: a fountain above the spawn, benches and lamps along the boulevard, trees on the grass.
	sx, sy := art(m.Spawn.X), art(m.Spawn.Y)
	add("fountain", sx-20, sy-58, none)

	for _, side := range []int{-1, 1} {
		add("bench", sx+side*70-12, sy-48, none)
		add("planter", sx+side*150-15, sy-52, none)
		add("planter", sx+side*260-15, sy+60, none)
		add("lamp", sx+side*110-3, sy-60, none)
		add("lamp", sx+side*330-3, sy-60, none)
		add("lamp", sx+side*210-3, sy+52, none)
	}

	W, H := art(m.Width), art(m.Height)
	for _, t := range [][2]int{
		{248, 30}, {272, 96}, {246, 150}, {520, 40}, {546, 110}, {516, 160}, // between the rooms
		{6, 250}, {W - 34, 250}, // the ends of the boulevard
		{40, 370}, {110, 400}, {180, 372}, {30, 470}, {150, 480}, {80, 540}, {196, 560}, {36, 596}, // left of the cinema
		{W - 68, 370}, {W - 138, 400}, {W - 208, 372}, {W - 58, 470}, {W - 178, 480}, {W - 108, 540}, {W - 224, 560}, {W - 64, 596},
	} {
		if t[1] < H-32 {
			add("tree", t[0], t[1], none)
		}
	}

	return out
}

var mapPNG = sync.OnceValue(func() []byte { return encode(Default.paint()) })

// MapPNG is the map's picture, Width×Height.
func MapPNG() []byte { return mapPNG() }

func encode(c canvas) []byte {
	b := c.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx()*Pixel, b.Dy()*Pixel))

	for y := range out.Rect.Dy() {
		for x := range out.Rect.Dx() {
			out.SetRGBA(x, y, c.RGBAAt(x/Pixel, y/Pixel))
		}
	}

	var buf bytes.Buffer
	(&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, out)

	return buf.Bytes()
}

func (m Map) paint() canvas {
	W, H := art(m.Width), art(m.Height)
	c := newCanvas(W, H)

	c.grass(W, H)
	c.paving(m, W)

	for _, z := range m.Zones {
		c.room(z)
	}

	pieces := m.furniture()

	// Flat things first, then by their feet: what stands lower on the map covers what is behind it.
	for _, p := range pieces {
		if p.kind == "rug" {
			c.piece(p)
		}
	}

	feet := func(p piece) int {
		if f := solid[p.kind]; !f.Empty() {
			return p.y + f.Max.Y
		}

		return p.y + 10
	}

	sort.SliceStable(pieces, func(i, j int) bool { return feet(pieces[i]) < feet(pieces[j]) })

	for _, p := range pieces {
		if p.kind != "rug" {
			c.piece(p)
		}
	}

	return c
}

func (c canvas) grass(W, H int) {
	base, dark, light := rgb(0x8ccf7e), rgb(0x76bd6c), rgb(0xa4dc8f)
	c.rect(0, 0, W, H, base)

	for y := range H {
		for x := range W {
			switch n := noise(x/2, y/2); {
			case n < 6:
				c.px(x, y, dark)
			case n > 95:
				c.px(x, y, light)
			}

			// A flower now and then.
			if noise(x, y+7919) == 0 && noise(x+31, y) < 12 {
				petal := []color.RGBA{rgb(0xffffff), rgb(0xffd866), rgb(0xff9ac1)}[noise(x, y+3)%3]
				c.px(x, y, petal)
				c.px(x+1, y, petal)
				c.px(x, y+1, rgb(0x5aa35a))
			}
		}
	}
}

// brick paves a rectangle like a sidewalk: rows of bricks, each row shifted.
func (c canvas) brick(x, y, w, h int) {
	base, grout := rgb(0xf0e2cb), rgb(0xdccbae)

	for j := y; j < y+h; j++ {
		row := j / 6
		for i := x; i < x+w; i++ {
			col := base
			shifted := i + (row%2)*6

			switch {
			case j%6 == 5 || shifted%12 == 11:
				col = grout
			case noise(shifted/12, row) < 14:
				col = rgb(0xe8d6ba)
			case noise(shifted/12, row) > 90:
				col = rgb(0xf6ecd9)
			}

			c.px(i, j, col)
		}
	}

	edge := rgb(0xc9b697)
	c.rect(x, y, w, 1, edge)
	c.rect(x, y+h-1, w, 1, edge)
}

// paving is the boulevard between the rooms and the cinema, with a path to each door.
func (c canvas) paving(m Map, W int) {
	top, bottom := 0, 0
	for _, z := range m.Zones {
		if z.Door == "top" {
			bottom = art(z.Y)
		} else {
			top = art(z.Y + z.H)
		}
	}

	c.brick(16, top+10, W-32, bottom-top-20)

	for _, z := range m.Zones {
		x := art(z.X+z.W/2) - art(door)/2 - 4
		if z.Door == "top" {
			c.brick(x, bottom-11, art(door)+8, 12)
		} else {
			c.brick(x, top-1, art(door)+8, 12)
		}
	}
}

func (c canvas) room(z Zone) {
	x, y, w, h := art(z.X), art(z.Y), art(z.W), art(z.H)
	accent := rgb(z.Color)
	side, top, gap := art(wall), art(topWall), art(door)
	doorX := x + (w-gap)/2

	// Floor.
	if z.Kind == "cinema" {
		carpet, dot := rgb(0x3a2147), rgb(0x4b2c5a)
		c.rect(x, y, w, h, carpet)

		for j := y; j < y+h; j++ {
			for i := x; i < x+w; i++ {
				if (i+j)%8 == 0 && (i-j+800)%8 == 0 {
					c.px(i, j, dot)
					c.px(i+1, j, dot)
				}
			}
		}

		// The aisle's red carpet, from the door to the screen.
		ax, aw := doorX+8, gap-16
		c.rect(ax, y, aw, art(z.Screen.Y-z.Y)-12, rgb(0xa8263c))
		c.rect(ax, y, 1, art(z.Screen.Y-z.Y)-12, rgb(0xe6b84a))
		c.rect(ax+aw-1, y, 1, art(z.Screen.Y-z.Y)-12, rgb(0xe6b84a))

		for j := y + top + 6; j < art(z.Screen.Y)-16; j += 12 {
			c.px(ax-3, j, rgb(0xffe08a))
			c.px(ax+aw+2, j, rgb(0xffe08a))
		}
	} else {
		base := mix(rgb(0x2a2e3a), accent, 0.06)
		alt := mix(rgb(0x343948), accent, 0.08)
		grout := rgb(0x21242e)

		for j := y; j < y+h; j++ {
			for i := x; i < x+w; i++ {
				col := base
				if ((i-x)/16+(j-y)/16)%2 == 1 {
					col = alt
				}

				if (i-x)%16 == 15 || (j-y)%16 == 15 {
					col = grout
				}

				c.px(i, j, col)
			}
		}

		// A line of the room's colour around the floor.
		line := mix(base, accent, 0.55)
		c.rect(x+side+6, y+top+6, w-2*side-12, 1, line)
		c.rect(x+side+6, y+h-side-7, w-2*side-12, 1, line)
		c.rect(x+side+6, y+top+6, 1, h-top-side-12, line)
		c.rect(x+w-side-7, y+top+6, 1, h-top-side-12, line)
	}

	// Walls: the top one shows its face, with the room's name; the others are seen from above.
	cap, face := rgb(0x454b60), rgb(0x6b7391)
	if z.Kind == "cinema" {
		cap, face = rgb(0x3b2a48), rgb(0x64457a)
	}

	segment := func(sx, sw int) {
		c.rect(sx, y, sw, top, face)
		c.rect(sx, y, sw, 5, cap)
		c.rect(sx, y+5, sw, 1, mix(face, rgb(0xffffff), 0.3))
		c.rect(sx, y+top-3, sw, 2, accent)
		c.rect(sx, y+top-1, sw, 1, mix(face, rgb(0x000000), 0.4))
		c.shade(sx, y+top, sw, 3, 0.28)
	}

	if z.Door == "top" {
		segment(x, doorX-x)
		segment(doorX+gap, x+w-doorX-gap)
		c.rect(doorX-2, y, 2, top, cap)
		c.rect(doorX+gap, y, 2, top, cap)
		c.rect(x, y+h-side, w, side, cap)
	} else {
		segment(x, w)
		c.rect(x, y+h-side, doorX-x, side, cap)
		c.rect(doorX+gap, y+h-side, x+w-doorX-gap, side, cap)
		c.rect(x, y+h-side, doorX-x, 1, mix(cap, rgb(0xffffff), 0.2))
		c.rect(doorX+gap, y+h-side, x+w-doorX-gap, 1, mix(cap, rgb(0xffffff), 0.2))

		// A doormat in the room's colour.
		c.rect(doorX+8, y+h-side-1, gap-16, side+2, mix(accent, rgb(0x000000), 0.25))
		c.rect(doorX+10, y+h-side, gap-20, side, accent)
	}

	c.rect(x, y, side, h, cap)
	c.rect(x+w-side, y, side, h, cap)
	c.rect(x, y, 1, h, mix(cap, rgb(0xffffff), 0.2))
	c.shade(x+side, y+top, 2, h-top-side, 0.18)

	// The sign: a dark plate with the name lit in the room's colour.
	name := upper(z.Name)
	tw := len(name)*6 - 1
	signs := []int{x + (w-tw)/2}
	if z.Door == "top" {
		signs = []int{x + (doorX-x-tw)/2, doorX + gap + (x+w-doorX-gap-tw)/2}
	}

	for _, sx := range signs {
		c.rect(sx-4, y+6, tw+8, 7, ink)
		c.rect(sx-5, y+7, tw+10, 5, ink)
		c.write(sx, y+6, name, mix(accent, rgb(0xffffff), 0.35))
	}

	// Posters and pennants on what is left of the wall.
	for i, px := range []int{x + side + 8, x + w - side - 20} {
		if z.Kind == "cinema" {
			c.rect(px, y+6, 10, 8, ink)
			c.rect(px+1, y+7, 8, 6, []color.RGBA{rgb(0xe6b84a), rgb(0x4aa3e6)}[i])
			c.rect(px+3, y+9, 4, 3, rgb(0xfff3d6))

			continue
		}

		for k := range 3 {
			c.rect(px+k*4, y+6, 3, 5-k%2, mix(accent, rgb(0xffffff), float32(k)*0.2))
			c.px(px+k*4+1, y+11-k%2, mix(accent, rgb(0xffffff), float32(k)*0.2))
		}
	}
}

func (c canvas) piece(p piece) {
	x, y, a := p.x, p.y, p.accent

	switch p.kind {
	case "desk":
		c.shade(x+1, y+19, 29, 2, 0.3)
		c.box(x, y+5, 30, 14, 4, rgb(0xe9e4dc)) // a white top, as in an office

		// Monitor, lit in the room's colour.
		c.rect(x+4, y, 15, 9, ink)
		c.rect(x+5, y+1, 13, 6, mix(a, rgb(0x10131c), 0.55))
		c.rect(x+5, y+1, 13, 2, mix(a, rgb(0xffffff), 0.15))
		c.rect(x+7, y+4, 5, 1, rgb(0xffffff))
		c.rect(x+7, y+6, 8, 1, mix(a, rgb(0xffffff), 0.5))
		c.rect(x+10, y+9, 3, 1, rgb(0x3b3f4e))

		// Keyboard, mouse, a mug.
		c.rect(x+5, y+11, 11, 3, rgb(0x2b2e3a))
		for k := range 5 {
			c.px(x+6+k*2, y+12, mix(a, rgb(0xffffff), 0.3))
		}

		c.rect(x+18, y+12, 2, 2, rgb(0x2b2e3a))
		c.rect(x+1, y+8, 3, 3, mix(a, rgb(0xffffff), 0.2))

		// The tower, with its lights.
		c.rect(x+23, y+1, 6, 13, ink)
		c.rect(x+24, y+2, 4, 11, rgb(0x2b2e3a))
		c.rect(x+25, y+3, 1, 8, a)
		c.px(x+27, y+3, rgb(0x7dffb0))

	case "chair":
		c.shade(x+1, y+11, 10, 1, 0.3)
		c.rect(x+1, y, 10, 4, rgb(0x23252f))
		c.rect(x+2, y+1, 8, 2, mix(a, rgb(0x23252f), 0.35))
		c.rect(x, y+3, 12, 7, rgb(0x2f3240))
		c.rect(x+1, y+4, 10, 5, rgb(0x3c4051))
		c.rect(x, y+5, 1, 4, rgb(0x191a22))
		c.rect(x+11, y+5, 1, 4, rgb(0x191a22))

	case "rug":
		c.rect(x, y, 76, 48, mix(a, rgb(0x1b1c26), 0.55))
		c.rect(x+2, y+2, 72, 44, mix(a, rgb(0x1b1c26), 0.35))
		c.rect(x+4, y+4, 68, 40, mix(a, rgb(0x1b1c26), 0.5))
		for i := 0; i < 68; i += 4 {
			c.px(x+4+i, y+6, mix(a, rgb(0xffffff), 0.3))
			c.px(x+6+i, y+41, mix(a, rgb(0xffffff), 0.3))
		}

	case "sofa":
		cloth := mix(a, rgb(0xffffff), 0.1)
		dark := mix(a, rgb(0x000000), 0.3)
		c.shade(x+1, y+16, 43, 2, 0.3)
		c.rect(x, y, 44, 16, dark)
		c.rect(x+1, y+1, 42, 5, mix(cloth, rgb(0x000000), 0.12)) // the back
		c.rect(x+4, y+6, 36, 8, cloth)
		c.rect(x+15, y+6, 1, 8, dark)
		c.rect(x+28, y+6, 1, 8, dark)
		c.rect(x+1, y+5, 3, 10, mix(cloth, rgb(0x000000), 0.2))
		c.rect(x+40, y+5, 3, 10, mix(cloth, rgb(0x000000), 0.2))
		c.rect(x+6, y+7, 5, 4, rgb(0xfff3d6)) // a cushion

	case "table":
		c.shade(x+1, y+11, 19, 2, 0.3)
		c.box(x, y, 20, 11, 3, rgb(0xc98d5a))
		c.rect(x+4, y+2, 4, 3, rgb(0xffffff))
		c.rect(x+11, y+2, 5, 4, ink)
		c.rect(x+12, y+3, 3, 2, a)

	case "arcade":
		c.shade(x+1, y+24, 14, 2, 0.3)
		c.rect(x, y, 14, 24, ink)
		c.rect(x+1, y+1, 12, 4, a)
		c.rect(x+2, y+6, 10, 7, rgb(0x10131c))
		c.rect(x+3, y+7, 3, 2, rgb(0x7dffb0))
		c.rect(x+8, y+10, 2, 2, rgb(0xffd866))
		c.rect(x+1, y+14, 12, 3, rgb(0x3c4051))
		c.px(x+4, y+15, rgb(0xff5d5d))
		c.px(x+8, y+15, rgb(0x5db2ff))
		c.px(x+10, y+15, rgb(0xffd866))
		c.rect(x+1, y+18, 12, 5, mix(a, rgb(0x000000), 0.45))

	case "vending":
		c.shade(x+1, y+26, 16, 2, 0.3)
		c.rect(x, y, 16, 26, ink)
		c.rect(x+1, y+1, 14, 24, rgb(0x3d6fd9))
		c.rect(x+2, y+2, 9, 16, rgb(0x16233f))
		for row := range 4 {
			for col := range 3 {
				can := []color.RGBA{rgb(0xff5d5d), rgb(0x7dffb0), rgb(0xffd866)}[(row+col)%3]
				c.rect(x+3+col*3, y+3+row*4, 2, 3, can)
			}
		}

		c.rect(x+12, y+4, 2, 5, rgb(0xcfd6e6))
		c.rect(x+2, y+20, 12, 3, rgb(0x16233f))

	case "plant":
		c.shade(x+2, y+17, 9, 1, 0.3)
		c.rect(x+3, y+11, 6, 6, rgb(0xc9703f))
		c.rect(x+2, y+10, 8, 2, rgb(0xe08a55))
		c.disc(x+6, y+6, 5, 1, rgb(0x3f9d57))
		c.disc(x+3, y+7, 3, 1, rgb(0x4fb468))
		c.disc(x+9, y+5, 3, 1, rgb(0x62c77a))
		c.px(x+6, y+3, rgb(0x8fe39f))
		c.px(x+4, y+6, rgb(0x8fe39f))

	case "tree":
		c.disc(x+14, y+31, 11, 0.4, mix(rgb(0x8ccf7e), rgb(0x000000), 0.25))
		c.rect(x+12, y+22, 5, 9, rgb(0x8a5a3a))
		c.rect(x+12, y+22, 2, 9, rgb(0xa9744d))
		c.disc(x+14, y+12, 12, 1, rgb(0x3d9a55))
		c.disc(x+8, y+15, 8, 1, rgb(0x358a4c))
		c.disc(x+20, y+15, 8, 1, rgb(0x358a4c))
		c.disc(x+13, y+9, 9, 1, rgb(0x56b96a))
		c.disc(x+10, y+6, 5, 1, rgb(0x78d184))
		c.px(x+17, y+11, rgb(0x9be3a0))
		c.px(x+7, y+13, rgb(0x78d184))

	case "fountain":
		c.disc(x+20, y+19, 20, 0.7, mix(rgb(0xf0e2cb), rgb(0x000000), 0.2))
		c.disc(x+20, y+17, 20, 0.7, rgb(0xb9bfce))
		c.disc(x+20, y+17, 18, 0.7, rgb(0xdfe4ee))
		c.disc(x+20, y+17, 15, 0.7, rgb(0x4aa3e6))
		c.disc(x+20, y+16, 11, 0.7, rgb(0x6fc0f5))
		c.disc(x+20, y+15, 4, 0.7, rgb(0xdfe4ee))
		c.rect(x+19, y+6, 3, 9, rgb(0xb9bfce))
		c.rect(x+18, y+4, 5, 2, rgb(0xdfe4ee))
		c.px(x+17, y+7, rgb(0xffffff))
		c.px(x+23, y+8, rgb(0xffffff))
		c.px(x+12, y+18, rgb(0xffffff))
		c.px(x+28, y+14, rgb(0xffffff))
		c.px(x+24, y+21, rgb(0xc9ecff))

	case "bench":
		c.shade(x+1, y+9, 23, 2, 0.25)
		c.rect(x, y, 24, 3, rgb(0x9a6240))
		c.rect(x, y+4, 24, 4, rgb(0xc98d5a))
		c.rect(x, y+4, 24, 1, rgb(0xe0a876))
		c.rect(x+1, y+8, 2, 1, ink)
		c.rect(x+21, y+8, 2, 1, ink)

	case "lamp":
		c.disc(x+3, y+3, 7, 1, mix(rgb(0xf0e2cb), rgb(0xffe08a), 0.5))
		c.rect(x+2, y+4, 2, 13, rgb(0x3b3f4e))
		c.rect(x+1, y+16, 4, 2, rgb(0x2b2e3a))
		c.rect(x, y, 6, 5, rgb(0x2b2e3a))
		c.rect(x+1, y+1, 4, 3, rgb(0xfff3b0))

	case "planter":
		c.shade(x+1, y+12, 29, 2, 0.25)
		c.box(x, y+3, 30, 9, 4, rgb(0xb9bfce))
		for i := range 5 {
			c.disc(x+4+i*5, y+3, 3, 1, []color.RGBA{rgb(0x3f9d57), rgb(0x56b96a)}[i%2])
		}

		c.px(x+6, y+1, rgb(0xff9ac1))
		c.px(x+15, y+2, rgb(0xffd866))
		c.px(x+23, y+1, rgb(0xffffff))

	case "seat":
		red := rgb(0xc23148)
		c.shade(x+1, y+11, 10, 1, 0.3)
		c.rect(x, y+3, 12, 8, mix(red, rgb(0x000000), 0.45))
		c.rect(x+1, y+2, 10, 7, red)
		c.rect(x+2, y+3, 8, 3, mix(red, rgb(0xffffff), 0.18))
		c.rect(x+1, y+8, 10, 3, mix(red, rgb(0x000000), 0.25)) // the back, toward the door
		c.rect(x, y+4, 1, 5, ink)
		c.rect(x+11, y+4, 1, 5, ink)

	case "popcorn":
		c.shade(x+1, y+26, 18, 2, 0.3)
		c.rect(x, y+6, 18, 20, rgb(0x8a1f2f))
		c.rect(x+1, y+16, 16, 9, rgb(0xd13a4f))
		c.rect(x+1, y+18, 16, 1, rgb(0xffffff))
		c.rect(x+1, y+22, 16, 1, rgb(0xffffff))
		c.rect(x+1, y+7, 16, 9, rgb(0xcfe9f5))
		c.rect(x+2, y+10, 14, 6, rgb(0xffe08a))
		for i := range 6 {
			c.px(x+3+i*2, y+9+i%2, rgb(0xfff6cf))
		}

		c.rect(x, y+3, 18, 3, rgb(0xd13a4f))
		c.rect(x+2, y+1, 14, 2, rgb(0xffe08a))

	case "counter":
		c.shade(x+1, y+18, 40, 2, 0.3)
		c.box(x, y+2, 40, 16, 6, rgb(0x7a5a8c))
		c.rect(x, y+12, 40, 1, rgb(0xe6b84a))
		c.rect(x+4, y+4, 4, 5, rgb(0xd13a4f))
		c.rect(x+4, y+3, 4, 2, rgb(0xffe08a))
		c.rect(x+12, y+4, 3, 5, rgb(0x4aa3e6))
		c.rect(x+13, y+2, 1, 2, rgb(0xffffff))
		c.rect(x+20, y+4, 3, 5, rgb(0xff8a3d))
		c.rect(x+28, y+3, 8, 6, ink)
		c.rect(x+29, y+4, 6, 3, rgb(0x7dffb0))

	case "speaker":
		c.shade(x+1, y+30, 12, 2, 0.3)
		c.rect(x, y, 12, 30, ink)
		c.rect(x+1, y+1, 10, 28, rgb(0x2b2e3a))
		c.disc(x+6, y+8, 3, 1, rgb(0x50556a))
		c.disc(x+6, y+8, 1, 1, ink)
		c.disc(x+6, y+20, 4, 1, rgb(0x50556a))
		c.disc(x+6, y+20, 2, 1, ink)

	case "screen":
		// The picture itself is the client's: here go the frame, the curtains and the stage light.
		c.shade(x-16, y+126, 256, 8, 0.35)
		c.rect(x-3, y-3, 230, 132, rgb(0x8a8fa3))
		c.rect(x-2, y-2, 228, 130, ink)
		c.rect(x, y, 224, 126, rgb(0x0c0d12))

		for _, cx := range []int{x - 14, x + 228} {
			for i := range 10 {
				red := rgb(0xa8263c)
				if i%3 == 0 {
					red = rgb(0x7d1a2c)
				} else if i%3 == 1 {
					red = rgb(0xc93a52)
				}

				c.rect(cx+i, y-8, 1, 140, red)
			}

			c.rect(cx-1, y-9, 12, 3, rgb(0xe6b84a))
		}
	}
}

// A 5×7 font with the letters the room names use.
var glyphs = map[rune][7]string{
	'A': {".###.", "#...#", "#...#", "#####", "#...#", "#...#", "#...#"},
	'C': {".####", "#....", "#....", "#....", "#....", "#....", ".####"},
	'D': {"####.", "#...#", "#...#", "#...#", "#...#", "#...#", "####."},
	'E': {"#####", "#....", "#....", "####.", "#....", "#....", "#####"},
	'F': {"#####", "#....", "#....", "####.", "#....", "#....", "#...."},
	'G': {".####", "#....", "#....", "#.###", "#...#", "#...#", ".###."},
	'I': {"#####", "..#..", "..#..", "..#..", "..#..", "..#..", "#####"},
	'L': {"#....", "#....", "#....", "#....", "#....", "#....", "#####"},
	'M': {"#...#", "##.##", "#.#.#", "#.#.#", "#...#", "#...#", "#...#"},
	'N': {"#...#", "##..#", "#.#.#", "#..##", "#...#", "#...#", "#...#"},
	'O': {".###.", "#...#", "#...#", "#...#", "#...#", "#...#", ".###."},
	'R': {"####.", "#...#", "#...#", "####.", "#.#..", "#..#.", "#...#"},
	'S': {".####", "#....", "#....", ".###.", "....#", "....#", "####."},
	'T': {"#####", "..#..", "..#..", "..#..", "..#..", "..#..", "..#.."},
	'U': {"#...#", "#...#", "#...#", "#...#", "#...#", "#...#", ".###."},
	'V': {"#...#", "#...#", "#...#", "#...#", ".#.#.", ".#.#.", "..#.."},
	'2': {".###.", "#...#", "....#", "...#.", "..#..", ".#...", "#####"},
}

func upper(s string) string {
	b := []byte(s)
	for i, ch := range b {
		if ch >= 'a' && ch <= 'z' {
			b[i] = ch - 32
		}
	}

	return string(b)
}

// write draws text; a letter the font lacks is left blank.
func (c canvas) write(x, y int, text string, col color.RGBA) {
	for i, r := range text {
		for row, line := range glyphs[r] {
			for k, bit := range line {
				if bit == '#' {
					c.px(x+i*6+k, y+row, col)
				}
			}
		}
	}
}
