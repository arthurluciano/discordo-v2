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
	tones  = []uint32{0xffdcb8, 0xf1c27d, 0xd9a066, 0xa8703f, 0x6f4a2a}
	hairs  = []uint32{0x2b1d14, 0x5a3825, 0x8a5a2b, 0xd9a441, 0xe8e1d0, 0xc2452d, 0x3d6fd9, 0xd957a8}
	shirts = []uint32{0x5865f2, 0xed4245, 0x3ba55d, 0xfaa81a, 0xeb459e, 0x00b0f4, 0x9b59b6, 0xf2f3f5, 0x23272a, 0x1abc9c}
	pants  = []uint32{0x2f3a5f, 0x3b3f4e, 0x6b4a2f, 0x23272a}
	kits   = []uint32{0x2b2e3a, 0xe9e4dc, 0xd13a4f, 0x3d6fd9, 0xf2a93b, 0x3ba55d}
)

// SpritePNG is the sheet of the character for a seed: a skin's ID gives that skin, anything else (a user's ID)
// a person made up from it.
func SpritePNG(seed string) []byte {
	if s, ok := skins[seed]; ok {
		return s.look.sheet()
	}

	h := fnv.New32a()
	h.Write([]byte(seed))
	n := h.Sum32()

	pick := func(list []uint32) color.RGBA {
		v := list[n%uint32(len(list))]
		n = n/uint32(len(list)) ^ n<<7

		return rgb(v)
	}

	return look{skin: pick(tones), hair: pick(hairs), shirt: pick(shirts), pants: pick(pants), kit: pick(kits),
		style: int(n>>9) % 7, top: int(n>>15) % 4, gear: int(n>>19) % 6}.sheet()
}

func (l look) sheet() []byte {
	sheet := newCanvas(20*3, 26*4)

	for row, facing := range []int{down, left, right, up} {
		for step := range 3 {
			f := l.frame(facing, step)
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
	style                    int        // 0 short, 1 long, 2 spiky, 3 cap, 4 bun, 5 curls, 6 ponytail
	top                      int        // 0 a tee, 1 a hoodie, 2 an open jacket, 3 stripes
	gear                     int        // 0 nothing, 1 glasses, 2 headphones, 3 a beard, 4 freckles, 5 a backpack
	kit                      color.RGBA // the gear's colour

	// What a skin adds to a made-up person.
	eyes  color.RGBA              // ink when unset
	plain bool                    // no blush: masks and hoods
	extra func(r pen, facing int) // drawn over the figure; left is drawn as right and mirrored
}

// pen draws a rectangle in the figure's own coordinates: the head starts at 3,2 and the feet end at row 20.
type pen func(x, y, w, h int, col color.RGBA)

// frame draws one pose. Everything is placed for facing right or down; left is right mirrored.
func (l look) frame(facing, step int) canvas {
	c := newCanvas(20, 26)
	ox, oy := 2, 3 // room for the outline, the spikes and the buns
	r := func(x, y, w, h int, col color.RGBA) { c.rect(ox+x, oy+y, w, h, col) }

	dark := func(col color.RGBA) color.RGBA { return mix(col, rgb(0x000000), 0.25) }
	light := func(col color.RGBA) color.RGBA { return mix(col, rgb(0xffffff), 0.28) }
	shoe, sole := rgb(0x2b2e3a), rgb(0xd9dbe3)
	eye := l.eyes
	if eye.A == 0 {
		eye = ink
	}
	hat := mix(l.shirt, rgb(0xffffff), 0.15)
	blush := mix(l.skin, rgb(0xff7a7a), 0.4)
	inner := rgb(0xeceef3) // under an open jacket

	// Steps: one leg lifts and the arms swing the other way.
	lift := [2]int{}
	swing := 0
	if step == 1 {
		lift[0], swing = 1, 1
	} else if step == 2 {
		lift[1], swing = 1, -1
	}

	if facing == right || facing == left {
		// Legs: apart on one step, together and lifted on the other; a light sole under each shoe.
		leg := func(x, y, h int, col color.RGBA) {
			r(x, y, 3, h, col)
			r(x, y+h, 4, 1, shoe)
			r(x, y+h+1, 4, 1, sole)
		}

		switch step {
		case 0:
			leg(6, 16, 2, l.pants)
		case 1:
			leg(9, 16, 2, dark(l.pants))
			leg(4, 16, 2, l.pants)
		case 2:
			leg(6, 16, 1, l.pants)
		}

		// The body: lit from the front, the back in shade.
		r(5, 10, 6, 6, l.shirt)
		r(5, 10, 1, 6, dark(l.shirt))
		r(10, 11, 1, 4, light(l.shirt))
		r(5, 15, 6, 1, dark(l.pants)) // a belt
		switch l.top {
		case 1: // a hoodie: the hood behind the neck
			r(5, 10, 3, 2, dark(l.shirt))
		case 2: // an open jacket
			r(9, 10, 2, 5, inner)
		case 3: // stripes
			r(6, 12, 5, 1, light(l.shirt))
			r(6, 14, 5, 1, light(l.shirt))
		}

		ax := 7 + swing // the arm, and the hand under it
		r(ax, 11, 2, 4, dark(l.shirt))
		r(ax, 11, 2, 1, mix(dark(l.shirt), rgb(0x000000), 0.15))
		r(ax, 15, 2, 1, l.skin)

		// The head: a round shape, the face to the right, a nose, an ear in the middle.
		r(4, 3, 9, 6, l.skin)
		r(5, 2, 7, 8, l.skin)
		r(13, 6, 1, 1, l.skin)
		r(5, 9, 6, 1, dark(l.skin))
		r(7, 5, 1, 2, dark(l.skin))
		r(10, 5, 1, 2, eye)
		r(10, 5, 1, 1, light(eye))
		if !l.plain {
			r(11, 8, 1, 1, mix(l.skin, rgb(0x8a3a3a), 0.5))
			r(11, 7, 1, 1, blush)
		}

		r(5, 0, 7, 1, l.hair)
		r(4, 1, 9, 3, l.hair)
		r(4, 4, 3, 4, l.hair)
		r(12, 3, 1, 1, l.hair) // the fringe over the brow
		r(6, 1, 4, 1, light(l.hair))
		r(4, 3, 2, 1, dark(l.hair))

		switch l.style {
		case 1: // long
			r(3, 3, 3, 10, l.hair)
			r(3, 3, 1, 10, dark(l.hair))
		case 2: // spiky
			r(5, -1, 1, 1, l.hair)
			r(7, -2, 1, 2, l.hair)
			r(10, -1, 1, 1, l.hair)
			r(3, 1, 1, 1, l.hair)
		case 3: // a cap with its brim forward
			r(4, 0, 9, 3, hat)
			r(5, 0, 5, 1, light(hat))
			r(10, 3, 5, 1, dark(hat))
		case 4: // a bun at the back
			r(2, 0, 3, 3, l.hair)
			r(2, 0, 2, 1, light(l.hair))
		case 5: // curls
			r(3, -1, 10, 6, l.hair)
			r(3, 5, 4, 3, l.hair)
			for _, p := range [][2]int{{4, 0}, {7, -1}, {10, 0}, {5, 2}, {8, 1}, {4, 5}} {
				r(p[0], p[1], 1, 1, dark(l.hair))
			}
			r(6, 0, 1, 1, light(l.hair))
		case 6: // a ponytail with its tie
			r(2, 3, 2, 7, l.hair)
			r(2, 3, 1, 7, dark(l.hair))
			r(4, 3, 1, 1, hat)
		}
	} else {
		for i, lx := range []int{5, 9} {
			h := 3 - lift[i]
			r(lx, 16, 2, h, l.pants)
			r(lx, 16, 1, h, light(l.pants))
			r(lx, 16+h, 2, 1, shoe)
			r(lx, 17+h, 2, 1, sole)
		}

		// The body, lit from the left; the arms swing with the steps.
		r(4, 10, 8, 6, l.shirt)
		r(10, 10, 2, 6, dark(l.shirt))
		r(5, 10, 1, 5, light(l.shirt))
		r(4, 15, 8, 1, dark(l.pants)) // a belt
		r(7, 15, 2, 1, rgb(0xc9a24a)) // its buckle

		for i, ax := range []int{3, 12} {
			dy := swing
			if i == 1 {
				dy = -swing
			}

			r(ax, 10, 1, 5+min(dy, 0), dark(l.shirt))
			r(ax, 15+min(dy, 0), 1, 1, l.skin)
		}

		if facing == down {
			switch l.top {
			case 0:
				r(6, 10, 4, 1, dark(l.shirt)) // a collar
			case 1: // a hoodie: its strings and the pocket
				r(5, 10, 6, 1, dark(l.shirt))
				r(6, 11, 1, 2, rgb(0xf2f3f5))
				r(9, 11, 1, 2, rgb(0xf2f3f5))
				r(5, 13, 6, 2, dark(l.shirt))
			case 2: // an open jacket over a light shirt
				r(7, 10, 2, 5, inner)
				r(6, 10, 1, 5, dark(l.shirt))
				r(9, 10, 1, 5, dark(l.shirt))
			case 3: // stripes
				r(4, 12, 8, 1, light(l.shirt))
				r(4, 14, 8, 1, light(l.shirt))
				r(6, 10, 4, 1, dark(l.shirt))
			}
		} else if l.top == 1 {
			r(5, 10, 6, 2, dark(l.shirt)) // the hood, down
		}

		// The head: rounded corners, ears, the far side in shade.
		r(4, 2, 8, 8, l.skin)
		r(3, 3, 10, 6, l.skin)
		r(2, 5, 1, 2, l.skin)
		r(13, 5, 1, 2, l.skin)
		r(11, 3, 1, 6, dark(l.skin))
		r(5, 9, 6, 1, dark(l.skin))

		if facing == down {
			// Eyes with a light in them, a mouth, cheeks.
			for _, ex := range []int{5, 9} {
				r(ex, 5, 2, 2, eye)
				r(ex+1, 5, 1, 1, mix(eye, rgb(0xffffff), 0.75))
			}

			if !l.plain {
				r(7, 8, 2, 1, mix(l.skin, rgb(0x8a3a3a), 0.3))
				r(4, 7, 1, 1, blush)
				r(11, 7, 1, 1, blush)
			}

			r(4, 0, 8, 1, l.hair)
			r(3, 1, 10, 3, l.hair)
			r(3, 4, 1, 3, l.hair)
			r(12, 4, 1, 3, l.hair)
			r(4, 4, 2, 1, l.hair) // the fringe
			r(8, 4, 1, 1, l.hair)
			r(11, 4, 1, 1, l.hair)
			r(5, 1, 4, 1, light(l.hair))
			r(10, 2, 2, 1, dark(l.hair))
		} else {
			r(4, 0, 8, 1, l.hair)
			r(3, 1, 10, 8, l.hair)
			r(4, 9, 8, 1, dark(l.hair))
			r(5, 1, 4, 1, light(l.hair))
			r(11, 2, 1, 6, dark(l.hair))
		}

		switch l.style {
		case 1: // long
			r(2, 3, 2, 10, l.hair)
			r(12, 3, 2, 10, l.hair)
			r(2, 3, 1, 10, dark(l.hair))
			r(13, 3, 1, 10, dark(l.hair))
			if facing == up {
				r(3, 9, 10, 3, l.hair)
				r(3, 11, 10, 1, dark(l.hair))
			}
		case 2: // spiky
			r(4, -1, 1, 1, l.hair)
			r(6, -2, 1, 2, l.hair)
			r(9, -2, 1, 2, l.hair)
			r(11, -1, 1, 1, l.hair)
		case 3: // a cap
			r(3, 0, 10, 3, hat)
			r(5, 0, 4, 1, light(hat))
			r(7, -1, 2, 1, dark(hat))
			if facing == down {
				r(2, 3, 12, 1, dark(hat))
			} else {
				r(5, 3, 6, 1, dark(hat)) // the strap
			}
		case 4: // a bun on top
			r(6, -3, 4, 3, l.hair)
			r(6, -3, 2, 1, light(l.hair))
			r(6, -1, 4, 1, dark(l.hair))
		case 5: // curls, around the face
			r(2, -1, 12, 5, l.hair)
			r(1, 1, 2, 6, l.hair)
			r(13, 1, 2, 6, l.hair)
			if facing == up {
				r(2, 4, 12, 5, l.hair)
			}
			for _, p := range [][2]int{{3, 0}, {6, -1}, {9, 0}, {12, 1}, {1, 4}, {14, 3}, {5, 2}, {10, 2}} {
				r(p[0], p[1], 1, 1, dark(l.hair))
			}
			r(5, 0, 2, 1, light(l.hair))
		case 6: // a ponytail: from the front its tie, from the back all of it
			if facing == up {
				r(7, 8, 2, 5, l.hair)
				r(8, 8, 1, 5, dark(l.hair))
				r(7, 8, 2, 1, hat)
			} else {
				r(13, 2, 1, 2, hat)
				r(13, 4, 1, 4, l.hair)
			}
		}
	}

	l.wear(r, facing)

	if l.extra != nil {
		at := facing
		if at == left {
			at = right
		}

		l.extra(r, at)
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

			// The outline takes a dark shade of what it borders: softer than black, and it reads as one shape.
			for _, d := range []image.Point{{0, 1}, {1, 0}, {-1, 0}, {0, -1}} {
				if p := image.Pt(x, y).Add(d); p.In(c.Rect) && c.RGBAAt(p.X, p.Y).A != 0 {
					out.SetRGBA(x, y, mix(c.RGBAAt(p.X, p.Y), ink, 0.78))
					break
				}
			}
		}
	}

	return out
}

// wear draws the gear over the figure, facing right or down as frame does (left is mirrored after).
func (l look) wear(r pen, facing int) {
	if l.gear == 0 {
		return
	}

	dark := mix(l.kit, rgb(0x000000), 0.3)
	glass := rgb(0x2b2e3a)
	beard := mix(l.hair, rgb(0x000000), 0.15)
	side := facing == right || facing == left

	switch l.gear {
	case 1: // glasses
		switch {
		case side:
			r(8, 5, 2, 1, glass)
			r(11, 5, 1, 2, glass)
			r(10, 7, 1, 1, glass)
		case facing == down:
			r(7, 5, 2, 1, glass)
			r(3, 5, 1, 1, glass)
			r(12, 5, 1, 1, glass)
			r(5, 7, 2, 1, glass)
			r(9, 7, 2, 1, glass)
		}

	case 2: // headphones: a band over the head, a cup on each ear
		if side {
			r(6, -1, 4, 1, dark)
			r(6, 3, 3, 5, l.kit)
			r(6, 3, 1, 5, dark)
			return
		}

		r(4, -1, 8, 1, dark)
		r(2, 0, 2, 2, dark)
		r(12, 0, 2, 2, dark)
		r(1, 3, 2, 5, l.kit)
		r(13, 3, 2, 5, l.kit)
		r(1, 3, 1, 5, dark)
		r(14, 3, 1, 5, dark)

	case 3: // a beard and a moustache
		if l.plain {
			return
		}

		switch {
		case side:
			r(8, 8, 4, 2, beard)
			r(7, 7, 1, 2, beard)
			r(11, 7, 2, 1, beard)
		case facing == down:
			r(4, 9, 8, 1, beard)
			r(3, 7, 1, 2, beard)
			r(12, 7, 1, 2, beard)
			r(6, 8, 4, 1, beard)
			r(5, 10, 6, 1, beard)
		}

	case 4: // freckles
		if l.plain || facing == up {
			return
		}

		spot := mix(l.skin, rgb(0x8a4a2a), 0.45)
		if side {
			r(9, 7, 1, 1, spot)
			r(11, 6, 1, 1, spot)
			return
		}

		for _, p := range [][2]int{{5, 7}, {6, 8}, {9, 8}, {10, 7}} {
			r(p[0], p[1], 1, 1, spot)
		}

	case 5: // a backpack: its straps from the front, all of it from the back
		switch {
		case side:
			r(2, 10, 3, 6, l.kit)
			r(2, 10, 1, 6, dark)
			r(3, 12, 2, 1, dark)
		case facing == down:
			r(5, 10, 1, 5, dark)
			r(10, 10, 1, 5, dark)
		default:
			r(4, 10, 8, 6, l.kit)
			r(4, 10, 8, 1, dark)
			r(5, 12, 6, 3, mix(l.kit, rgb(0xffffff), 0.15))
			r(5, 12, 6, 1, dark)
			r(7, 13, 2, 1, rgb(0xc9a24a))
		}
	}
}
