package world

import (
	"hash/fnv"
	"image"
	"image/color"
)

// A character: a small person drawn from a seed (the user's ID), so everyone sees the same one for each user.
// The sheet has a row for each way they face (down, left, right, up) and three columns: standing and two steps.

const (
	SpriteW = 20 * Pixel // one frame
	SpriteH = 26 * Pixel
)

var (
	skins  = []uint32{0xffdcb8, 0xf1c27d, 0xd9a066, 0xa8703f, 0x6f4a2a}
	hairs  = []uint32{0x2b1d14, 0x5a3825, 0x8a5a2b, 0xd9a441, 0xe8e1d0, 0xc2452d, 0x3d6fd9, 0xd957a8}
	shirts = []uint32{0x5865f2, 0xed4245, 0x3ba55d, 0xfaa81a, 0xeb459e, 0x00b0f4, 0x9b59b6, 0xf2f3f5, 0x23272a, 0x1abc9c}
	pants  = []uint32{0x2f3a5f, 0x3b3f4e, 0x6b4a2f, 0x23272a}
)

// SpritePNG is the sheet of the character for a seed.
func SpritePNG(seed string) []byte {
	h := fnv.New32a()
	h.Write([]byte(seed))
	n := h.Sum32()

	pick := func(list []uint32) color.RGBA {
		v := list[n%uint32(len(list))]
		n = n/uint32(len(list)) ^ n<<7

		return rgb(v)
	}

	look := look{skin: pick(skins), hair: pick(hairs), shirt: pick(shirts), pants: pick(pants), style: int(n>>9) % 4}
	sheet := newCanvas(20*3, 26*4)

	for row, facing := range []int{down, left, right, up} {
		for step := range 3 {
			f := look.frame(facing, step)
			for y := range 26 {
				for x := range 20 {
					if col := f.RGBAAt(x, y); col.A != 0 {
						sheet.SetRGBA(step*20+x, row*26+y, col)
					}
				}
			}
		}
	}

	return encode(sheet)
}

const (
	down = iota
	left
	right
	up
)

type look struct {
	skin, hair, shirt, pants color.RGBA
	style                    int // 0 short, 1 long, 2 spiky, 3 cap
}

// frame draws one pose. Everything is placed for facing right or down; left is right mirrored.
func (l look) frame(facing, step int) canvas {
	c := newCanvas(20, 26)
	ox, oy := 2, 3 // room for the outline and the spikes
	r := func(x, y, w, h int, col color.RGBA) { c.rect(ox+x, oy+y, w, h, col) }

	dark := func(col color.RGBA) color.RGBA { return mix(col, rgb(0x000000), 0.25) }
	shoe := rgb(0x2b2e3a)
	hat := mix(l.shirt, rgb(0xffffff), 0.15)

	// Steps: one leg lifts, and the body rises a pixel with it.
	lift := [2]int{}
	bob := 0
	if step == 1 {
		lift[0], bob = 1, 0
	} else if step == 2 {
		lift[1], bob = 1, 0
	}

	side := facing == right || facing == left

	if side {
		// Legs: apart on one step, together and lifted on the other.
		switch step {
		case 0:
			r(6, 16, 4, 4, l.pants)
			r(6, 20, 5, 1, shoe)
		case 1:
			r(4, 16, 3, 4, l.pants)
			r(9, 16, 3, 4, dark(l.pants))
			r(4, 20, 4, 1, shoe)
			r(9, 20, 4, 1, shoe)
		case 2:
			r(6, 16, 4, 3, l.pants)
			r(6, 19, 5, 1, shoe)
		}

		r(5, 10, 6, 6, l.shirt)
		r(5, 15, 6, 1, dark(l.shirt))
		r(7, 11, 2, 4, dark(l.shirt)) // the arm
		r(7, 15, 2, 1, l.skin)

		r(4, 2, 9, 8, l.skin)
		r(4, 0, 9, 4, l.hair)
		r(4, 4, 3, 5, l.hair)
		r(10, 5, 1, 2, ink) // the eye
		r(11, 8, 1, 1, dark(l.skin))

		switch l.style {
		case 1:
			r(4, 4, 3, 9, l.hair)
		case 2:
			r(5, -1, 1, 1, l.hair)
			r(8, -1, 1, 1, l.hair)
			r(11, -1, 1, 1, l.hair)
		case 3:
			r(4, 0, 9, 3, hat)
			r(10, 3, 5, 1, dark(hat))
		}
	} else {
		for i, lx := range []int{5, 9} {
			r(lx, 16, 2, 4-lift[i], l.pants)
			r(lx, 20-lift[i], 2, 1, shoe)
		}

		r(4, 10+bob, 8, 6, l.shirt)
		r(4, 15, 8, 1, dark(l.shirt))
		r(3, 10+bob, 1, 5, dark(l.shirt))
		r(12, 10+bob, 1, 5, dark(l.shirt))
		r(3, 15, 1, 1, l.skin)
		r(12, 15, 1, 1, l.skin)

		r(3, 2, 10, 8, l.skin)

		if facing == down {
			r(3, 0, 10, 4, l.hair)
			r(3, 4, 1, 3, l.hair)
			r(12, 4, 1, 3, l.hair)
			r(6, 5, 1, 2, ink)
			r(9, 5, 1, 2, ink)
			r(4, 7, 1, 1, mix(l.skin, rgb(0xff7a7a), 0.4))
			r(11, 7, 1, 1, mix(l.skin, rgb(0xff7a7a), 0.4))
			r(7, 11+bob, 2, 1, dark(l.shirt)) // a collar
		} else {
			r(3, 0, 10, 9, l.hair)
			r(5, 9, 6, 1, l.skin)
		}

		switch l.style {
		case 1:
			r(2, 3, 2, 10, l.hair)
			r(12, 3, 2, 10, l.hair)
			if facing == up {
				r(3, 9, 10, 3, l.hair)
			}
		case 2:
			r(4, -1, 1, 1, l.hair)
			r(7, -1, 2, 1, l.hair)
			r(11, -1, 1, 1, l.hair)
		case 3:
			r(3, 0, 10, 3, hat)
			if facing == down {
				r(2, 3, 12, 1, dark(hat))
			}
		}
	}

	if facing == left {
		for y := range 26 {
			for x := range 10 {
				a, b := c.RGBAAt(x, y), c.RGBAAt(19-x, y)
				c.SetRGBA(x, y, b)
				c.SetRGBA(19-x, y, a)
			}
		}
	}

	// A dark line around the figure makes it stand out on any floor, and a shadow puts it on the ground.
	out := newCanvas(20, 26)
	for y := range 3 {
		for x := 4; x < 16; x++ {
			if (y != 0 && y != 2) || (x > 5 && x < 14) {
				out.SetRGBA(x, 22+y, color.RGBA{0, 0, 0, 70})
			}
		}
	}

	for y := range 26 {
		for x := range 20 {
			if c.RGBAAt(x, y).A != 0 {
				out.SetRGBA(x, y, c.RGBAAt(x, y))
				continue
			}

			for _, d := range []image.Point{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				if p := image.Pt(x, y).Add(d); p.In(c.Rect) && c.RGBAAt(p.X, p.Y).A != 0 {
					out.SetRGBA(x, y, ink)
					break
				}
			}
		}
	}

	return out
}
