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
	"screen":   image.Rect(-16, -14, 240, 132), // with the curtains and the stage in front
	"booth":    image.Rect(0, 2, 28, 14),
	"post":     image.Rect(0, 4, 3, 12),
	"planter":  image.Rect(0, 2, 30, 12),
	"pine":     image.Rect(9, 24, 17, 32),
	"blossom":  image.Rect(8, 22, 20, 32),
	"bush":     image.Rect(1, 3, 15, 10),
	"hedge":    image.Rect(0, 2, 32, 9),
	"pond":     image.Rect(2, 4, 118, 60),
	"sign":     image.Rect(2, 14, 6, 20),
	"bin":      image.Rect(0, 2, 7, 10),
	"cart":     image.Rect(0, 6, 32, 22),
	"shelf":    image.Rect(0, 2, 26, 20),
	"tv":       image.Rect(0, 4, 44, 14),
	"cooler":   image.Rect(0, 4, 9, 20),
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
			sx, sy, sw := art(s.X), art(s.Y), art(s.W)
			doorX := x + (w-art(door))/2

			add("screen", sx, sy, accent)
			add("speaker", sx-34, sy+24, accent)
			add("speaker", sx+sw+22, sy+24, accent)
			add("speaker", sx-34, sy+76, accent)
			add("speaker", sx+sw+22, sy+76, accent)

			// Three tiers on each side of the aisle, the back rows wider, as in a theatre.
			for row := range 3 {
				n := 7 + row/2
				for i := range n {
					add("seat", doorX-20-i*15, y+46+row*19, accent)
					add("seat", doorX+art(door)+8+i*15, y+46+row*19, accent)
				}
			}

			// The lobby by the door: snacks on one side, the projection booth and the queue's posts on the other.
			add("popcorn", x+10, y+20, accent)
			add("counter", x+32, y+22, accent)
			add("booth", x+w-44, y+20, accent)
			add("plant", x+w-60, y+24, accent)
			add("post", doorX+2, y+18, accent)
			add("post", doorX+art(door)-5, y+18, accent)

			for _, sy := range []int{y + 60, y + 150, y + 230} {
				add("sconce", x+art(wall)-1, sy, accent)
				add("sconce", x+w-art(wall)-3, sy, accent)
			}

			add("exit", x+art(wall)+4, y+h-art(wall)-10, accent)
			add("exit", x+w-art(wall)-16, y+h-art(wall)-10, accent)

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
		add("beanbag", x+120, y+94, accent)
		add("beanbag", x+170, y+94, accent)
		add("tv", x+132, y+114, accent)
		add("arcade", x+10, y+h-36, accent)
		add("vending", x+28, y+h-38, accent)
		add("shelf", x+50, y+h-38, accent)
		add("cooler", x+80, y+h-36, accent)
		add("plant", x+w-22, y+h-30, accent)
		add("plant", x+w-22, y+70, accent)
	}

	// The plaza: a fountain above the spawn ringed by benches, lamps and a signpost; hedges along the edges.
	sx, sy := art(m.Spawn.X), art(m.Spawn.Y)
	add("fountain", sx-20, sy-58, none)
	add("sign", sx+28, sy-12, none)

	for _, side := range []int{-1, 1} {
		add("bench", sx+side*58-12, sy-46, none)
		add("bench", sx+side*58-12, sy-14, none)
		add("lamp", sx+side*92-3, sy-60, none)
		add("lamp", sx+side*330-3, sy-60, none)
		add("lamp", sx+side*210-3, sy+52, none)
		add("bin", sx+side*100-3, sy-20, none)
		add("planter", sx+side*260-15, sy+60, none)
	}

	add("cart", sx+150, sy-6, none)

	top, bottom := 0, 0
	var doors []int
	for _, z := range m.Zones {
		doors = append(doors, art(z.X+z.W/2))
		if z.Door == "top" {
			bottom = art(z.Y)
		} else {
			top = art(z.Y + z.H)
		}
	}

	W, H := art(m.Width), art(m.Height)
	for hx := 24; hx+32 <= W-24; hx += 32 {
		free := true
		for _, d := range doors {
			if hx+32 > d-art(door)/2-12 && hx < d+art(door)/2+12 {
				free = false
			}
		}

		if free {
			add("hedge", hx, top+12, none)
			add("hedge", hx, bottom-20, none)
		}
	}

	// The pond and its trees on the left, a small wood of pines and blossoms on the right.
	add("pond", 40, 470, none)

	for _, t := range []struct {
		kind string
		x, y int
	}{
		{"tree", 248, 30}, {"pine", 274, 92}, {"tree", 246, 150}, {"tree", 520, 40}, {"pine", 548, 104}, {"blossom", 516, 156},
		{"tree", 6, 250}, {"tree", W - 34, 250},
		{"blossom", 40, 380}, {"tree", 110, 404}, {"pine", 184, 372}, {"tree", 186, 480}, {"pine", 6, 440}, {"blossom", 178, 560}, {"tree", 10, 590},
		{"pine", W - 68, 368}, {"tree", W - 138, 400}, {"blossom", W - 208, 372}, {"pine", W - 58, 470}, {"tree", W - 178, 480},
		{"pine", W - 108, 540}, {"blossom", W - 224, 560}, {"tree", W - 64, 596},
	} {
		if t.y < H-32 {
			add(t.kind, t.x, t.y, none)
		}
	}

	for _, b := range [][2]int{{230, 20}, {300, 160}, {500, 10}, {580, 160}, {60, 352}, {216, 420}, {150, 600}, {W - 90, 352}, {W - 236, 430}, {W - 150, 610}, {W - 30, 520}} {
		add("bush", b[0], b[1], none)
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

	for i, z := range m.Zones {
		c.room(z, i)
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

	// Soft round patches of darker and lighter grass.
	for gy := 0; gy < H; gy += 36 {
		for gx := 0; gx < W; gx += 36 {
			n := noise(gx, gy+101)
			if n > 45 {
				continue
			}

			col := mix(base, dark, 0.4)
			if n%2 == 0 {
				col = mix(base, light, 0.4)
			}

			c.disc(gx+n%20, gy+(n*7)%20, 8+n%10, 0.6, col)
		}
	}

	for y := range H {
		for x := range W {
			switch n := noise(x/2, y/2); {
			case n < 5:
				c.px(x, y, dark)
			case n > 96:
				c.px(x, y, light)
			}

			// Tufts: a little V of blades.
			if noise(x+17, y+3) < 1 {
				c.px(x, y, dark)
				c.px(x+2, y, dark)
				c.px(x+1, y+1, dark)
				c.px(x+1, y, light)
			}

			// A flower now and then, a pebble more rarely.
			if noise(x, y+7919) == 0 && noise(x+31, y) < 14 {
				petal := []color.RGBA{rgb(0xffffff), rgb(0xffd866), rgb(0xff9ac1), rgb(0xb59cff), rgb(0xff8a5c)}[noise(x, y+3)%5]
				c.px(x, y, petal)
				c.px(x+1, y, petal)
				c.px(x, y-1, petal)
				c.px(x+1, y-1, mix(petal, rgb(0xffe08a), 0.6))
				c.px(x, y+1, rgb(0x5aa35a))
			}

			if noise(x+101, y+59) == 0 && noise(x, y+11) < 6 {
				c.rect(x, y, 3, 2, rgb(0xb9bfce))
				c.px(x, y, rgb(0xdfe4ee))
				c.rect(x, y+2, 3, 1, mix(base, rgb(0x000000), 0.25))
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

// slabs paves a rectangle with large stone slabs in two tones, each row offset, edged by a curb.
func (c canvas) slabs(x, y, w, h int) {
	base, alt, grout := rgb(0xeadcc4), rgb(0xe0cfb2), rgb(0xcdb999)

	for j := y; j < y+h; j++ {
		row := (j - y) / 10
		for i := x; i < x+w; i++ {
			shifted := i + (row%2)*8
			col := base
			if noise(shifted/16, row)%3 == 0 {
				col = alt
			}

			switch {
			case (j-y)%10 == 9 || shifted%16 == 15:
				col = grout
			case (j-y)%10 == 0 || shifted%16 == 0:
				col = mix(col, rgb(0xffffff), 0.25)
			case noise(i, j) < 3:
				col = mix(col, grout, 0.5)
			}

			c.px(i, j, col)
		}
	}
}

// paving is the boulevard between the rooms and the cinema, a round square at its centre, and a path to each door.
func (c canvas) paving(m Map, W int) {
	top, bottom := 0, 0
	for _, z := range m.Zones {
		if z.Door == "top" {
			bottom = art(z.Y)
		} else {
			top = art(z.Y + z.H)
		}
	}

	px, py, pw, ph := 16, top+10, W-32, bottom-top-20
	c.slabs(px, py, pw, ph)

	// A border of darker stone just inside a grey curb.
	curb, band := rgb(0x9aa0b2), rgb(0xc9a77a)
	c.rect(px, py, pw, 2, curb)
	c.rect(px, py+ph-2, pw, 2, curb)
	c.rect(px, py+2, pw, 1, mix(curb, rgb(0x000000), 0.2))
	for _, by := range []int{py + 3, py + ph - 7} {
		c.rect(px, by, pw, 4, band)
		for i := px; i < px+pw; i += 6 {
			c.px(i, by, mix(band, rgb(0xffffff), 0.3))
			c.px(i+3, by+3, mix(band, rgb(0x000000), 0.2))
		}
	}

	// The round square: rings of stone around the fountain, a terracotta band, rays to the edge.
	sx, sy := art(m.Spawn.X), art(m.Spawn.Y)-38
	for j := -56; j <= 56; j++ {
		for i := -80; i <= 80; i++ {
			d := float32(i*i)/1.0 + float32(j*j)*2.0
			x, y := sx+i, sy+j
			if y < py+8 || y >= py+ph-8 {
				continue
			}

			switch {
			case d > 76*76:
			case d > 72*72:
				c.px(x, y, rgb(0xa8673f))
			case d > 68*68:
				c.px(x, y, rgb(0xc98552))
			case d > 66*66:
				c.px(x, y, rgb(0xa8673f))
			default:
				col := rgb(0xf3e8d4)
				if (i+j+800)%8 < 4 != ((i-j+800)%8 < 4) {
					col = rgb(0xe9dcc3)
				}

				c.px(x, y, col)
			}
		}
	}

	// Paths of slabs to every door.
	for _, z := range m.Zones {
		x := art(z.X+z.W/2) - art(door)/2 - 4
		if z.Door == "top" {
			c.slabs(x, bottom-11, art(door)+8, 12)
		} else {
			c.slabs(x, top-1, art(door)+8, 12)
		}
	}
}

func (c canvas) room(z Zone, n int) {
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

		// The tiers: each row of seats stands on a step, its edge lit.
		for row := range 3 {
			ty := y + 44 + row*19
			c.rect(x+side, ty, w-2*side, 17, mix(carpet, rgb(0x000000), 0.18+float32(row)*0.06))
			c.rect(x+side, ty+17, w-2*side, 1, mix(carpet, rgb(0xffffff), 0.12))
			for i := x + side + 3; i < x+w-side; i += 9 {
				c.px(i, ty+17, rgb(0xffd27a))
			}
		}

		// The projector's beam, from the booth's window to the screen: a faint light over the floor.
		sx, sy, sw := art(z.Screen.X), art(z.Screen.Y), art(z.Screen.W)
		bx, by := x+w-30, y+top+4
		for j := by; j < sy-14; j++ {
			t := float32(j-by) / float32(sy-14-by)
			l := int(float32(bx-2) + (float32(sx+sw/2-40)-float32(bx-2))*t)
			r := int(float32(bx+2) + (float32(sx+sw/2+40)-float32(bx+2))*t)
			for i := l; i <= r; i++ {
				if image.Pt(i, j).In(c.Rect) {
					c.SetRGBA(i, j, mix(c.RGBAAt(i, j), rgb(0xcdb8ff), 0.10))
				}
			}
		}

		// The aisle's red carpet, from the door to the stage.
		ax, aw := doorX+8, gap-16
		c.rect(ax, y, aw, sy-y-14, rgb(0xa8263c))
		c.rect(ax+2, y, aw-4, sy-y-14, rgb(0xb72d44))
		c.rect(ax, y, 1, sy-y-14, rgb(0xe6b84a))
		c.rect(ax+aw-1, y, 1, sy-y-14, rgb(0xe6b84a))

		for j := y + top + 6; j < sy-18; j += 8 {
			c.px(ax-3, j, rgb(0xffe08a))
			c.px(ax+aw+2, j, rgb(0xffe08a))
		}
	} else {
		base := mix(rgb(0x2a2e3a), accent, 0.06)
		alt := mix(rgb(0x343948), accent, 0.08)
		grout := rgb(0x21242e)

		// Each room its own floor: tiles, wooden planks, or a carpet of diamonds.
		for j := y; j < y+h; j++ {
			for i := x; i < x+w; i++ {
				col := base
				switch n % 3 {
				case 0:
					if ((i-x)/16+(j-y)/16)%2 == 1 {
						col = alt
					}

					if (i-x)%16 == 15 || (j-y)%16 == 15 {
						col = grout
					} else if (i-x)%16 == 0 || (j-y)%16 == 0 {
						col = mix(col, rgb(0xffffff), 0.06)
					}

				case 1:
					wood := []color.RGBA{rgb(0x6b4a33), rgb(0x76523a), rgb(0x5f412d)}
					row := (j - y) / 6
					col = wood[noise((i-x+row*13)/28, row)%3]
					switch {
					case (j-y)%6 == 5, (i-x+row*13)%28 == 27:
						col = rgb(0x4a3323)
					case noise(i, j) < 4:
						col = mix(col, rgb(0x4a3323), 0.5) // the grain
					}

				default:
					col = mix(rgb(0x2b2d40), accent, 0.12)
					u, v := (i-x+j-y)%12, (i-x-(j-y)+1200)%12
					if u == 0 || v == 0 {
						col = mix(col, accent, 0.25)
					} else if (u < 6) == (v < 6) {
						col = mix(col, rgb(0x000000), 0.12)
					}
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

	// Windows on the wall's face, either side of the sign: the sky, a frame, a glint.
	if z.Kind != "cinema" {
		for _, wx := range []int{x + 34, x + 64, x + w - 76, x + w - 46} {
			c.rect(wx, y+5, 14, 8, rgb(0x3b3f4e))
			c.rect(wx+1, y+6, 12, 6, rgb(0x9fd6f5))
			c.rect(wx+1, y+9, 12, 3, rgb(0xc9ecff))
			c.rect(wx+7, y+6, 1, 6, rgb(0x3b3f4e))
			c.px(wx+2, y+7, rgb(0xffffff))
			c.px(wx+3, y+6, rgb(0xffffff))
			c.rect(wx-1, y+13, 16, 1, mix(face, rgb(0xffffff), 0.3))
		}
	}

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

		if z.Kind == "cinema" { // a marquee: bulbs above and below the name
			for i := sx - 4; i < sx+tw+4; i += 3 {
				c.px(i, y+4, rgb(0xffe08a))
				c.px(i+1, y+14, rgb(0xffe08a))
			}
		}
	}

	// Posters and pennants on what is left of the wall.
	for i, px := range []int{x + side + 8, x + w - side - 20} {
		if z.Kind == "cinema" {
			// Two film posters in gold frames: a sunset over hills, and a rocket under the stars.
			px += []int{0, -4}[i]
			c.rect(px-1, y+3, 13, 12, rgb(0xe6b84a))
			c.rect(px, y+4, 11, 10, ink)
			if i == 0 {
				c.rect(px, y+4, 11, 5, rgb(0xff8a5c))
				c.rect(px, y+7, 11, 2, rgb(0xffc36b))
				c.disc(px+5, y+8, 2, 1, rgb(0xfff3b0))
				c.rect(px, y+10, 11, 4, rgb(0x5a3b7a))
				c.rect(px+2, y+9, 4, 1, rgb(0x5a3b7a))
			} else {
				c.rect(px, y+4, 11, 10, rgb(0x1d2450))
				c.px(px+2, y+5, rgb(0xffffff))
				c.px(px+8, y+6, rgb(0xffffff))
				c.px(px+1, y+10, rgb(0xffffff))
				c.rect(px+5, y+6, 2, 5, rgb(0xe9e4dc))
				c.px(px+5, y+5, rgb(0xd13a4f))
				c.px(px+6, y+5, rgb(0xd13a4f))
				c.px(px+4, y+10, rgb(0xd13a4f))
				c.px(px+7, y+10, rgb(0xd13a4f))
				c.rect(px+5, y+11, 2, 2, rgb(0xff8a3d))
			}

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
		leaf, deep, lit := rgb(0x3d9a55), rgb(0x2f7d45), rgb(0x78d184)
		c.disc(x+14, y+31, 11, 0.4, mix(rgb(0x8ccf7e), rgb(0x000000), 0.25))
		c.rect(x+12, y+21, 5, 10, rgb(0x8a5a3a))
		c.rect(x+12, y+21, 2, 10, rgb(0xa9744d))
		c.rect(x+11, y+29, 7, 2, rgb(0x6f4a2f)) // roots
		c.disc(x+14, y+13, 12, 1, deep)
		c.disc(x+8, y+16, 8, 1, deep)
		c.disc(x+20, y+16, 8, 1, deep)
		c.disc(x+14, y+11, 11, 1, leaf)
		c.disc(x+9, y+13, 7, 1, leaf)
		c.disc(x+19, y+13, 7, 1, leaf)
		c.disc(x+12, y+7, 6, 1, mix(leaf, lit, 0.5))
		c.disc(x+10, y+5, 3, 1, lit)
		for _, p := range [][2]int{{17, 9}, {6, 13}, {21, 15}, {14, 17}, {9, 18}, {19, 5}} {
			c.px(x+p[0], y+p[1], []color.RGBA{lit, deep}[(p[0]+p[1])%2])
		}

	case "pine":
		c.disc(x+13, y+31, 9, 0.4, mix(rgb(0x8ccf7e), rgb(0x000000), 0.25))
		c.rect(x+11, y+25, 4, 6, rgb(0x7a4a2a))
		for i, w := range []int{4, 7, 10, 12} {
			ty := y + 2 + i*6
			for j := 0; j < 8; j++ {
				half := w * (j + 1) / 8
				c.rect(x+13-half, ty+j, 2*half, 1, rgb(0x2f7d55))
				c.rect(x+13-half, ty+j, half, 1, rgb(0x3f9a68))
			}
			c.rect(x+13-w, ty+7, 2*w, 1, rgb(0x24624a))
		}
		c.px(x+12, y+1, rgb(0x78d1a0))
		c.px(x+13, y, rgb(0x78d1a0))

	case "blossom":
		pink, deep, lit := rgb(0xf4a6c6), rgb(0xd77aa3), rgb(0xffd6e6)
		c.disc(x+14, y+31, 11, 0.4, mix(rgb(0x8ccf7e), rgb(0x000000), 0.25))
		c.rect(x+12, y+20, 4, 11, rgb(0x7a4a3a))
		c.rect(x+12, y+20, 1, 11, rgb(0x9a6a55))
		c.rect(x+15, y+18, 4, 1, rgb(0x7a4a3a))
		c.disc(x+14, y+12, 11, 1, deep)
		c.disc(x+8, y+15, 7, 1, deep)
		c.disc(x+20, y+15, 7, 1, deep)
		c.disc(x+13, y+10, 10, 1, pink)
		c.disc(x+11, y+7, 5, 1, lit)
		for _, p := range [][2]int{{18, 8}, {6, 12}, {21, 14}, {15, 16}, {9, 17}, {16, 4}, {4, 16}} {
			c.px(x+p[0], y+p[1], rgb(0xffffff))
		}
		c.px(x+4, y+30, pink) // fallen petals
		c.px(x+22, y+29, pink)
		c.px(x+18, y+32, lit)

	case "bush":
		c.disc(x+8, y+10, 7, 0.4, mix(rgb(0x8ccf7e), rgb(0x000000), 0.25))
		c.disc(x+5, y+6, 5, 1, rgb(0x358a4c))
		c.disc(x+11, y+6, 5, 1, rgb(0x358a4c))
		c.disc(x+8, y+4, 5, 1, rgb(0x3f9d57))
		c.disc(x+6, y+3, 2, 1, rgb(0x78d184))
		c.px(x+11, y+5, rgb(0xff6b6b)) // berries
		c.px(x+4, y+7, rgb(0xff6b6b))
		c.px(x+9, y+8, rgb(0xff6b6b))

	case "hedge":
		c.shade(x, y+9, 32, 2, 0.25)
		c.rect(x, y+2, 32, 7, rgb(0x2f7d45))
		c.rect(x, y+1, 32, 3, rgb(0x3f9d57))
		c.rect(x, y+1, 32, 1, rgb(0x56b96a))
		for i := 0; i < 32; i += 3 {
			c.px(x+i, y+noise(x+i, y)%3+3, rgb(0x56b96a))
			c.px(x+i+1, y+7, rgb(0x24624a))
		}
		c.px(x+7, y+2, rgb(0xffffff))
		c.px(x+21, y+3, rgb(0xffd866))

	case "pond":
		// Water inside a ring of stones, with lily pads, reeds and a duck.
		c.disc(x+60, y+33, 58, 0.5, rgb(0x9aa0b2))
		c.disc(x+60, y+32, 56, 0.5, rgb(0x4aa3e6))
		c.disc(x+60, y+34, 52, 0.45, rgb(0x3b8fd1))
		c.disc(x+58, y+28, 34, 0.35, rgb(0x6fc0f5))
		for i := 0; i < 120; i += 7 {
			c.px(x+2+i, y+33+noise(i, y)%3-1, rgb(0xdfe4ee))
		}
		for _, p := range [][2]int{{30, 26}, {80, 38}, {50, 44}} {
			c.disc(x+p[0], y+p[1], 4, 0.6, rgb(0x56b96a))
			c.px(x+p[0], y+p[1]-1, rgb(0x3b8fd1))
		}
		c.px(x+31, y+24, rgb(0xff9ac1))
		c.px(x+32, y+24, rgb(0xffd6e6))
		for _, rx := range []int{8, 11, 104, 108, 111} {
			c.rect(x+rx, y+10, 1, 10, rgb(0x3f9d57))
			c.rect(x+rx, y+8, 1, 3, rgb(0x8a5a3a))
		}
		c.rect(x+70, y+20, 6, 4, rgb(0xffffff)) // the duck
		c.rect(x+75, y+17, 3, 3, rgb(0x3f9d57))
		c.px(x+78, y+18, rgb(0xf2a93b))
		c.rect(x+68, y+24, 9, 1, rgb(0x6fc0f5))

	case "fountain":
		stone, lit, deep := rgb(0xb9bfce), rgb(0xdfe4ee), rgb(0x8a8fa3)
		c.disc(x+20, y+20, 21, 0.7, mix(rgb(0xf0e2cb), rgb(0x000000), 0.22))
		c.disc(x+20, y+18, 21, 0.7, deep)
		c.disc(x+20, y+17, 20, 0.7, stone)
		c.disc(x+20, y+16, 18, 0.7, lit)
		c.disc(x+20, y+17, 16, 0.7, rgb(0x3b8fd1))
		c.disc(x+20, y+16, 13, 0.7, rgb(0x4aa3e6))
		c.disc(x+18, y+14, 7, 0.6, rgb(0x6fc0f5))
		// The upper basin on its column, spilling into the lower one.
		c.rect(x+18, y+8, 5, 9, deep)
		c.rect(x+18, y+8, 2, 9, stone)
		c.disc(x+20, y+8, 7, 0.5, stone)
		c.disc(x+20, y+7, 6, 0.5, rgb(0x4aa3e6))
		c.rect(x+19, y, 3, 7, rgb(0xc9ecff))
		c.rect(x+20, y-2, 1, 3, rgb(0xffffff))
		for _, p := range [][2]int{{14, 6}, {26, 6}, {12, 9}, {28, 9}} {
			c.px(x+p[0], y+p[1], rgb(0xc9ecff))
		}
		for _, p := range [][2]int{{8, 18}, {31, 15}, {24, 22}, {13, 21}, {28, 20}} {
			c.px(x+p[0], y+p[1], rgb(0xffffff))
		}

	case "bench":
		wood, dark := rgb(0xc98d5a), rgb(0x9a6240)
		c.shade(x+1, y+9, 23, 2, 0.25)
		c.rect(x, y, 24, 3, dark)
		c.rect(x, y, 24, 1, mix(dark, rgb(0xffffff), 0.2))
		c.rect(x, y+4, 24, 4, wood)
		c.rect(x, y+4, 24, 1, rgb(0xe0a876))
		c.rect(x, y+6, 24, 1, dark)
		for _, lx := range []int{1, 21} {
			c.rect(x+lx, y+3, 2, 6, rgb(0x3b3f4e))
			c.px(x+lx, y+3, rgb(0x6b7391))
		}

	case "lamp":
		c.disc(x+3, y+3, 8, 1, mix(rgb(0xf0e2cb), rgb(0xffe08a), 0.45))
		c.disc(x+3, y+3, 5, 1, mix(rgb(0xf0e2cb), rgb(0xfff3b0), 0.6))
		c.shade(x+1, y+18, 5, 1, 0.3)
		c.rect(x+2, y+5, 2, 12, rgb(0x2b2e3a))
		c.px(x+2, y+6, rgb(0x50556a))
		c.rect(x+1, y+15, 4, 3, rgb(0x2b2e3a))
		c.rect(x, y+1, 6, 5, rgb(0x2b2e3a))
		c.rect(x+1, y+2, 4, 3, rgb(0xfff3b0))
		c.px(x+1, y+2, rgb(0xffffff))
		c.rect(x+1, y, 4, 1, rgb(0x2b2e3a))
		c.px(x+2, y-1, rgb(0x2b2e3a))

	case "planter":
		c.shade(x+1, y+12, 29, 2, 0.25)
		c.box(x, y+3, 30, 9, 4, rgb(0xb9bfce))
		for i := range 5 {
			c.disc(x+4+i*5, y+3, 3, 1, []color.RGBA{rgb(0x3f9d57), rgb(0x56b96a)}[i%2])
		}

		c.px(x+6, y+1, rgb(0xff9ac1))
		c.px(x+15, y+2, rgb(0xffd866))
		c.px(x+23, y+1, rgb(0xffffff))

	case "beanbag":
		c.disc(x+7, y+9, 7, 0.5, mix(a, rgb(0x000000), 0.45))
		c.disc(x+7, y+7, 7, 0.7, mix(a, rgb(0x000000), 0.15))
		c.disc(x+6, y+6, 5, 0.7, a)
		c.disc(x+5, y+4, 2, 0.7, mix(a, rgb(0xffffff), 0.35))

	case "tv":
		// A wide screen on a low cabinet, a console and its controller beside it.
		c.shade(x+1, y+14, 44, 2, 0.3)
		c.box(x+2, y+8, 40, 6, 3, rgb(0x4a3a5c))
		c.rect(x+4, y, 36, 9, ink)
		c.rect(x+5, y+1, 34, 6, mix(a, rgb(0x10131c), 0.5))
		c.rect(x+5, y+1, 34, 1, mix(a, rgb(0xffffff), 0.2))
		c.rect(x+8, y+3, 10, 3, mix(a, rgb(0xffffff), 0.4)) // a game on screen
		c.rect(x+22, y+4, 4, 2, rgb(0xffe08a))
		c.rect(x+30, y+2, 6, 4, mix(a, rgb(0x000000), 0.2))
		c.rect(x+6, y+10, 8, 2, rgb(0xe9e4dc))
		c.px(x+12, y+10, rgb(0x7dffb0))
		c.rect(x+30, y+10, 6, 2, ink)
		c.px(x+31, y+10, a)

	case "shelf":
		// A bookcase of games and a trophy.
		c.shade(x+1, y+20, 26, 2, 0.3)
		c.rect(x, y, 26, 20, rgb(0x6b4a2f))
		c.rect(x+1, y+1, 24, 18, rgb(0x3a2a1c))
		for _, sy := range []int{6, 12} {
			c.rect(x+1, y+sy, 24, 1, rgb(0x8a5a3a))
		}
		for i := range 10 {
			col := []color.RGBA{a, rgb(0x4aa3e6), rgb(0xffe08a), rgb(0x7dd87d), rgb(0xe9e4dc)}[(i*3)%5]
			c.rect(x+2+i*2, y+2+(i%2), 2, 4-i%2, col)
			c.rect(x+3+i*2, y+8, 1, 4, mix(col, rgb(0x000000), 0.2))
		}
		c.rect(x+16, y+15, 4, 3, rgb(0xe6b84a)) // the trophy
		c.rect(x+17, y+13, 2, 2, rgb(0xffe08a))
		c.rect(x+4, y+15, 8, 3, rgb(0x9aa0b2))

	case "cooler":
		c.shade(x, y+20, 9, 1, 0.3)
		c.rect(x, y+9, 9, 11, rgb(0xe9e4dc))
		c.rect(x, y+9, 1, 11, rgb(0xffffff))
		c.rect(x+2, y+12, 2, 2, rgb(0x4aa3e6))
		c.rect(x+5, y+12, 2, 2, rgb(0xd13a4f))
		c.rect(x+1, y, 7, 9, rgb(0x6fc0f5))
		c.rect(x+2, y+1, 2, 7, rgb(0xc9ecff))
		c.rect(x+2, y-1, 5, 1, rgb(0x3b8fd1))

	case "sign":
		// A signpost with an arrow in each room's colour.
		c.shade(x+2, y+19, 5, 1, 0.3)
		c.rect(x+3, y+2, 2, 18, rgb(0x8a5a3a))
		c.rect(x+3, y+2, 1, 18, rgb(0xa9744d))
		for i, col := range []uint32{0xff5555, 0xc8a96e, 0xee9f3b, 0xa857f7} {
			ay := y + 2 + i*3
			if i%2 == 0 {
				c.rect(x+5, ay, 8, 2, rgb(col))
				c.px(x+13, ay, rgb(col))
			} else {
				c.rect(x-5, ay, 8, 2, rgb(col))
				c.px(x-6, ay, rgb(col))
			}
		}

	case "bin":
		c.shade(x, y+10, 8, 1, 0.3)
		c.rect(x, y+2, 7, 8, rgb(0x3b6f4e))
		c.rect(x+1, y+3, 1, 6, rgb(0x56906a))
		c.rect(x, y+1, 7, 2, rgb(0x2b2e3a))
		c.rect(x+2, y+5, 3, 2, rgb(0xdfe4ee))

	case "cart":
		// A popcorn and juice cart under a striped awning.
		c.shade(x+1, y+22, 32, 2, 0.3)
		c.box(x, y+10, 32, 12, 4, rgb(0xe9e4dc))
		c.rect(x+2, y+12, 12, 4, rgb(0xffe08a))
		c.rect(x+17, y+12, 4, 4, rgb(0xff8a5c))
		c.rect(x+22, y+12, 4, 4, rgb(0x7dd87d))
		c.rect(x+27, y+12, 3, 4, rgb(0xb59cff))
		for i := range 8 {
			col := rgb(0xd13a4f)
			if i%2 == 1 {
				col = rgb(0xffffff)
			}
			c.rect(x+i*4, y, 4, 6, col)
			c.px(x+i*4+1, y+6, col)
			c.px(x+i*4+2, y+6, col)
		}
		c.rect(x+1, y+6, 1, 4, rgb(0x6b7391))
		c.rect(x+30, y+6, 1, 4, rgb(0x6b7391))
		c.disc(x+5, y+23, 2, 1, ink)
		c.disc(x+27, y+23, 2, 1, ink)

	case "seat":
		// Seen from behind, facing the screen: the back on top, the cushion and the armrests under it.
		plush, dark := rgb(0xc23148), rgb(0x7d1a2c)
		c.shade(x+1, y+12, 12, 1, 0.3)
		c.rect(x+1, y, 11, 8, ink)
		c.rect(x+2, y+1, 9, 6, plush)
		c.rect(x+2, y+1, 9, 1, mix(plush, rgb(0xffffff), 0.3))
		c.rect(x+3, y+3, 1, 3, mix(plush, rgb(0xffffff), 0.15))
		c.rect(x+9, y+3, 1, 3, dark)
		c.rect(x+1, y+8, 11, 4, dark)
		c.rect(x+2, y+8, 9, 2, mix(plush, rgb(0x000000), 0.15))
		c.rect(x, y+5, 2, 7, rgb(0x2b2e3a))
		c.rect(x+11, y+5, 2, 7, rgb(0x2b2e3a))
		c.px(x, y+5, rgb(0x50556a))
		c.px(x+12, y+5, rgb(0x50556a))
		c.px(x+5, y+2, rgb(0xe6b84a)) // the seat's number plate
		c.px(x+6, y+2, rgb(0xe6b84a))

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
		c.rect(x+1, y+1, 10, 1, rgb(0x50556a))
		c.disc(x+6, y+8, 3, 1, rgb(0x50556a))
		c.disc(x+6, y+8, 1, 1, rgb(0xe6b84a))
		c.disc(x+6, y+20, 4, 1, rgb(0x50556a))
		c.disc(x+6, y+20, 2, 1, ink)
		c.px(x+5, y+19, rgb(0x6b7391))

	case "booth":
		// The projector on its stand, its lens lit toward the screen.
		c.shade(x+1, y+14, 28, 2, 0.3)
		c.box(x, y+4, 28, 10, 4, rgb(0x4a3a5c))
		c.rect(x+3, y, 18, 7, ink)
		c.rect(x+4, y+1, 16, 5, rgb(0x6b7391))
		c.rect(x+4, y+1, 16, 1, rgb(0x9aa2c0))
		c.disc(x+7, y-1, 3, 1, ink)
		c.disc(x+16, y-1, 3, 1, ink)
		c.disc(x+7, y-1, 1, 1, rgb(0x9aa2c0))
		c.disc(x+16, y-1, 1, 1, rgb(0x9aa2c0))
		c.rect(x+20, y+2, 3, 3, ink)
		c.rect(x+21, y+3, 2, 1, rgb(0xfff3b0))

	case "post":
		// A queue's stanchion: a brass post holding a velvet rope across the lobby.
		c.shade(x, y+12, 4, 1, 0.3)
		c.rect(x, y+11, 3, 1, rgb(0xb8892f))
		c.rect(x+1, y+3, 1, 8, rgb(0xe6b84a))
		c.rect(x, y+2, 3, 2, rgb(0xffe08a))
		c.rect(x+1, y+5, 1, 1, rgb(0xa8263c))

	case "sconce":
		// A wall lamp: a warm glow spilling down the wall.
		for j := -8; j <= 8; j++ {
			for i := -8; i <= 8; i++ {
				if d := i*i + j*j; d <= 64 && image.Pt(x+2+i, y+3+j).In(c.Rect) {
					c.SetRGBA(x+2+i, y+3+j, mix(c.RGBAAt(x+2+i, y+3+j), rgb(0xffd27a), 0.16*float32(64-d)/64))
				}
			}
		}
		c.disc(x+2, y+3, 4, 1, mix(rgb(0x3a2147), rgb(0xffd27a), 0.25))
		c.rect(x, y, 4, 5, rgb(0xb8892f))
		c.rect(x+1, y+1, 2, 3, rgb(0xfff3b0))

	case "exit":
		// The green exit sign over the corner.
		c.rect(x, y, 12, 6, ink)
		c.rect(x+1, y+1, 10, 4, rgb(0x2fbf71))
		c.rect(x+3, y+2, 2, 2, rgb(0xeafff3))
		c.rect(x+6, y+2, 4, 1, rgb(0xeafff3))
		c.px(x+9, y+3, rgb(0xeafff3))

	case "screen":
		// The picture itself is the client's: here go the frame, the stage, the curtains and the valance.
		c.shade(x-16, y+126, 256, 8, 0.35)

		// The stage, in front of the screen: wood with footlights.
		c.rect(x-16, y-14, 256, 10, rgb(0x6b4a2f))
		c.rect(x-16, y-14, 256, 1, rgb(0x9a6d45))
		c.rect(x-16, y-6, 256, 2, rgb(0x4a321f))
		for i := x - 12; i < x+236; i += 8 {
			c.rect(i, y-11, 2, 1, rgb(0x9a6d45))
			c.px(i+4, y-6, rgb(0xfff3b0))
		}

		c.rect(x-3, y-3, 230, 132, rgb(0x8a8fa3))
		c.rect(x-2, y-2, 228, 130, ink)
		c.rect(x, y, 224, 126, rgb(0x0c0d12))

		// Side curtains in folds, tied back with gold.
		for _, cx := range []int{x - 15, x + 227} {
			for i := range 12 {
				red := []color.RGBA{rgb(0x7d1a2c), rgb(0xa8263c), rgb(0xc93a52)}[i%3]
				c.rect(cx+i, y-4, 1, 136, red)
			}

			c.rect(cx, y+70, 12, 3, rgb(0xe6b84a))
			c.px(cx+5, y+73, rgb(0xffe08a))
			c.rect(cx-1, y-5, 14, 2, rgb(0xb8892f))
		}

		// The valance: a scalloped drape across the top, trimmed in gold.
		c.rect(x-16, y-4, 256, 6, rgb(0xa8263c))
		c.rect(x-16, y-4, 256, 1, rgb(0xc93a52))
		for i := x - 16; i < x+240; i += 8 {
			c.disc(i+4, y+2, 3, 0.6, rgb(0xa8263c))
			c.px(i+4, y+4, rgb(0xe6b84a))
		}
		c.rect(x-16, y-5, 256, 1, rgb(0xe6b84a))
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
