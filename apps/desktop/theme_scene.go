package main

// The animated backdrops of Neon and Dragão (design/theme-scene.tsx, design/scenes.css), drawn behind the
// workspace's translucent panes as one mesh of quads: gradients and blurred discs for the light, and the
// dragon's SVG drawn once into images (the framework cannot rotate a quad, so the wings' beat is a few
// baked angles stretched about the shoulder). Animated, the window wakes at 30 frames a second; still
// ("Tema estático" or the system's reduce motion), it draws the CSS's resting frame and stays idle.

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"math"
	"strings"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"

	"gonativeui/gpu"
	"gonativeui/img"
	"gonativeui/theme"
	"gonativeui/ui"
	"gonativeui/widget/base"
)

const sceneFrameNs = 1_000_000_000 / 30

// sceneQuads is reused frame after frame: the mesh borrows it until End.
var sceneQuads []gpu.Quad

func (d *Discordo) sceneStill() bool { return themeStill || d.k.Theme().Settings.ReduceMotion }

// sceneTime is the animation's clock in seconds, and asks for the next frame while it runs.
func (d *Discordo) sceneTime() (t float64, still bool) {
	if d.sceneStill() {
		return 0, true
	}

	c := d.k.Context()
	c.WakeIn(sceneFrameNs)

	return float64(c.Input().Time) / 1e9, false
}

func (d *Discordo) drawScene(W, H float32, q []gpu.Quad) {
	c := d.k.Context()
	sceneQuads = q
	theme.At(c, 0, 0, W, H)
	c.Mesh(base.Floating, ui.HashString(c.ParentKey(), "scene"), q)
}

func solid(x0, y0, x1, y1 float32, top, bottom uint32) gpu.Quad {
	return gpu.Quad{Rect: [4]float32{x0, y0, x1, y1}, Color: [4]uint32{top, top, bottom, bottom}}
}

// glow is radial-gradient(col, transparent g) at (x, y): a disc of half that blurred by a quarter.
func glow(x, y, gx, gy float32, col uint32) gpu.Quad {
	r := min(gx, gy) / 2

	return gpu.Quad{
		Rect:  [4]float32{x - gx/2, y - gy/2, x + gx/2, y + gy/2},
		Color: [4]uint32{col, col, col, col},
		Radii: [4]float32{r, r, r, r},
		Soft:  min(gx, gy) / 4,
	}
}

// fade multiplies a colour's opacity by a.
func fade(c uint32, a float64) uint32 { return c&^0xff | uint32(float64(c&0xff)*a+0.5) }

// pingPong is an "infinite alternate" CSS animation's eased progress (0 to 1 and back) at t.
func pingPong(t, period float64) float64 {
	u := math.Mod(t/period, 2)
	if u > 1 {
		u = 2 - u
	}

	return u * u * (3 - 2*u) // ease-in-out
}

func lerp(a, b, f float64) float64 { return a + (b-a)*f }

// neonScene: the wallpaper's two glows and grid, the scan lines, and the beam sweeping down.
func (d *Discordo) neonScene(W, H float32) {
	t, still := d.sceneTime()
	s := d.ui.scale
	q := append(sceneQuads[:0], solid(0, 0, W, H, canvas, canvas))

	g1 := 0.4 * float32(math.Hypot(float64(0.85*W), float64(0.9*H)))
	g2 := 0.45 * float32(math.Hypot(float64(0.9*W), float64(0.95*H)))
	q = append(q, glow(0.85*W, 0.1*H, 2*g1, 2*g1, 0xff2a6d59), glow(0.1*W, 0.95*H, 2*g2, 2*g2, 0x05d9e840))

	// The grid: a line every 48 points, and dark scan lines every 3.
	px := max(1, float32(math.Round(float64(s))))
	for x := float32(0); x < W; x += 48 * s {
		q = append(q, solid(x, 0, x+px, H, 0x05d9e812, 0x05d9e812))
	}

	for y := float32(0); y < H; y += 48 * s {
		q = append(q, solid(0, y, W, y+px, 0x05d9e812, 0x05d9e812))
	}

	for y := float32(0); y < H; y += 3 * s {
		q = append(q, solid(0, y, W, y+s, 0x00000038, 0x00000038))
	}

	bh, y := 0.22*H, float32(0)
	if !still {
		y = float32(lerp(-0.25, 1.05, math.Mod(t, 7)/7)) * H
	}

	q = append(q,
		solid(0, y, W, y+0.85*bh, 0x05d9e800, 0x05d9e824),
		solid(0, y+0.85*bh, W, y+bh, 0x05d9e824, 0xff2a6d59),
	)

	d.drawScene(W, H, q)
}

// dragonScene: the wallpaper's embers glow, the dragon hovering and beating its wings, its breath, and
// embers rising past it.
func (d *Discordo) dragonScene(W, H float32) {
	t, still := d.sceneTime()
	s := d.ui.scale
	q := append(sceneQuads[:0], solid(0, 0, W, H, canvas, canvas))

	// radial-gradient(ellipse at 70% 110%, …, transparent 55%), circle at 15% 10% to 45%.
	q = append(q, glow(0.7*W, 1.1*H, 2*0.55*1.414*0.7*W, 2*0.55*1.414*1.1*H, 0xe2552b73))
	g := 0.45 * float32(math.Hypot(float64(0.85*W), float64(0.9*H)))
	q = append(q, glow(0.15*W, 0.1*H, 2*g, 2*g, 0x78141459))

	// .dragon: right -4%, bottom 4%, min(72vw, 860px) wide, 4:3, at 62% opacity.
	const opacity = 0.62
	dw := min(0.72*W, 860*s)
	u := dw / 400 // a unit of the 400 × 300 drawing
	ox, oy := 1.04*W-dw, 0.96*H-0.75*dw

	if !still {
		oy += float32(lerp(-0.015, 0.02, pingPong(t, 5))) * 0.75 * dw
	}

	if art := d.dragonArt(); art != nil {
		// At rest the wings have no transform: angle 0, unstretched.
		far, near, farY, nearY := 0.4, 0.4, 1.0, 1.0
		if !still {
			far, near = pingPong(t+0.15, 2.2), pingPong(t, 2.2)
			farY, nearY = 1-0.5*far, 1-0.5*near
		}

		q = art.wing(d, q, &farWing, far, farY, ox, oy, u, opacity)
		q = art.part(d, q, 0, [4]float32{ox, oy, ox + dw, oy + 0.75*dw}, [4]float32{}, opacity)
		q = art.wing(d, q, &nearWing, near, nearY, ox, oy, u, opacity)
	}

	if !still {
		// .dragon-fire: a breath every 6 s, at the mouth.
		op, sc := dragonFire(math.Mod(t, 6) / 6)
		if op > 0 {
			cx, cy, r := ox+0.11*dw, oy+0.335*dw, 0.109*dw*float32(sc)
			q = append(q,
				glow(cx, cy, 2*r, 2*r, fade(0xff5a2859, op*opacity)),
				glow(cx, cy, r, r, fade(0xffbe5ad9, op*opacity)),
			)
		}

		q = embers(q, t, W, H, s)
	}

	d.drawScene(W, H, q)
}

// dragonFire is @keyframes dragon-fire at progress f: the opacity and the scale.
func dragonFire(f float64) (op, sc float64) {
	ease := func(a, b, f float64) float64 { return lerp(a, b, f*f*(3-2*f)) }

	switch {
	case f < 0.7:
		return 0, 0.4
	case f < 0.8:
		f = (f - 0.7) / 0.1
		return ease(0, 1, f), ease(0.4, 1, f)
	case f < 0.9:
		f = (f - 0.8) / 0.1
		return ease(1, 0.6, f), ease(1, 1.15, f)
	default:
		f = (f - 0.9) / 0.1
		return ease(0.6, 0, f), ease(1.15, 0.4, f)
	}
}

// embers are theme-scene.tsx's sixteen: fixed places, sizes and speeds, rising with a drift.
func embers(q []gpu.Quad, t float64, W, H, s float32) []gpu.Quad {
	for i := range 16 {
		size := float32(3+i%3) * s
		dur := 7 + float64(i%5)*1.3
		f := math.Mod(t+float64(i)*0.9, dur) / dur

		var op float64
		switch {
		case f < 0.1:
			op = 0.9 * f / 0.1
		case f < 0.8:
			op = lerp(0.9, 0.5, (f-0.1)/0.7)
		default:
			op = 0.5 * (1 - f) / 0.2
		}

		x := W*float32((i*37)%100)/100 + float32((i%7)-3)*18*s*float32(f)
		y := 1.02*H - size - 1.05*H*float32(f)
		r := size / 2

		// box-shadow 0 0 6px 2px, then the ember.
		q = append(q, gpu.Quad{
			Rect:  [4]float32{x - 2*s, y - 2*s, x + size + 2*s, y + size + 2*s},
			Color: [4]uint32{fade(0xff7828b3, op), fade(0xff7828b3, op), fade(0xff7828b3, op), fade(0xff7828b3, op)},
			Radii: [4]float32{r + 2*s, r + 2*s, r + 2*s, r + 2*s},
			Soft:  3 * s,
		})

		c := fade(0xffb347ff, op)
		q = append(q, gpu.Quad{Rect: [4]float32{x, y, x + size, y + size}, Color: [4]uint32{c, c, c, c}, Radii: [4]float32{r, r, r, r}})
	}

	return q
}

// The dragon's drawing, from theme-scene.tsx.

const dragonBody = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 300">
<defs><linearGradient id="skin" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#7a1a16"/><stop offset="1" stop-color="#2a0a0c"/></linearGradient></defs>
<g fill="url(#skin)" stroke="#ff6a3d" stroke-opacity="0.45" stroke-width="1.2" stroke-linejoin="round">
<path d="M92,86 C100,70 112,60 128,56 C114,66 106,76 100,90 Z"/>
<path d="M84,90 C88,74 96,64 108,58 C98,70 92,80 90,94 Z"/>
%s
<path d="M196,176 C194,196 186,206 176,214 L188,214 C200,206 208,194 212,178 Z"/>
<path d="M248,176 C252,196 246,210 236,220 L248,220 C260,210 266,194 264,174 Z"/>
<path d="M38,114 C50,104 64,98 80,96 L92,84 C100,90 108,96 116,104 C132,118 146,130 166,134 C196,138 224,128 250,136 C278,144 300,164 322,184 C344,204 368,212 392,204 L384,214 C358,226 330,218 306,200 C284,184 262,172 238,174 C214,178 190,180 170,170 C150,160 132,146 116,132 C104,122 92,118 80,120 C64,122 50,122 38,114 Z"/>
</g>
<circle cx="74" cy="104" r="3" fill="#ffd27a"/>
</svg>`

// wingArt is one wing: its drawing and the part of the 400 × 300 drawing (x0, y0, x1, y1) its beat sweeps.
type wingArt struct {
	part int
	svg  string
	crop [4]float32
}

var (
	farWing = wingArt{1, `<path d="M190,138 C176,100 160,60 140,20 C170,40 200,44 230,30 C228,62 236,92 252,118 C236,120 218,126 206,140 Z" fill="#3a0c10" stroke="#ff6a3d" stroke-opacity="0.25"/>`,
		[4]float32{120, 5, 265, 150}}
	nearWing = wingArt{2, `<g stroke="#ff7a45" stroke-opacity="0.5" stroke-width="1.2"><path d="M196,140 C200,96 216,50 250,10 Q272,48 326,46 Q296,82 320,124 Q276,116 250,134 Q222,128 196,140 Z" fill="#8f2419" fill-opacity="0.9"/><path d="M196,140 L326,46 M196,140 L320,124" fill="none"/></g>`,
		[4]float32{175, -5, 355, 150}}
)

// @keyframes dragon-flap turns the wing from -6° to 9° about the shoulder as it squashes it to half.
const (
	wingAngles = 8 // baked turns, in a 4 × 2 sheet
	wingCols   = 4
	wingRes    = 2   // pixels per unit of the drawing
	bodyRes    = 2.5 // the body is seen larger than the wings' parts
	shoulderX  = 192
	shoulderY  = 140
)

// sceneArt is the dragon's three images: the body and the two wings' sheets of turns.
type sceneArt struct {
	png  [3][]byte
	size [3][2]int32
	hash [3]uint64
}

var (
	dragon       *sceneArt
	dragonBaking bool
)

// dragonArt is the dragon's images once drawn; the first call starts drawing them on a goroutine.
func (d *Discordo) dragonArt() *sceneArt {
	if dragon == nil && !dragonBaking {
		dragonBaking = true

		go func() {
			art := bakeDragon()
			d.post(func(*Discordo) { dragon = art })
		}()
	}

	return dragon
}

func bakeDragon() *sceneArt {
	var art sceneArt
	var spines strings.Builder
	for x := 172; x <= 292; x += 24 {
		fmt.Fprintf(&spines, `<path d="M%d,%g l7,-13 l7,14 Z"/>`, x, 133+float64(x-172)/10)
	}

	art.set(0, fmt.Sprintf(dragonBody, spines.String()), 400*bodyRes, 300*bodyRes)

	for _, w := range [...]*wingArt{&farWing, &nearWing} {
		cw, ch := w.crop[2]-w.crop[0], w.crop[3]-w.crop[1]
		rows := wingAngles / wingCols

		var sheet strings.Builder
		fmt.Fprintf(&sheet, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %g %g">`, cw*wingCols, ch*float32(rows))

		for i := range wingAngles {
			tx := float32(i%wingCols)*cw - w.crop[0]
			ty := float32(i/wingCols)*ch - w.crop[1]
			fmt.Fprintf(&sheet, `<g transform="translate(%g %g) rotate(%g %d %d)">%s</g>`, tx, ty, wingAngle(i), shoulderX, shoulderY, w.svg)
		}

		sheet.WriteString(`</svg>`)
		art.set(w.part, sheet.String(), int(cw*wingRes*wingCols), int(ch*wingRes*float32(rows)))
	}

	return &art
}

func wingAngle(i int) float64 { return -6 + 15*float64(i)/(wingAngles-1) }

// set draws svg into a w × h PNG for the image cache.
func (a *sceneArt) set(i int, svg string, w, h int) {
	icon, err := oksvg.ReadIconStream(strings.NewReader(svg), oksvg.IgnoreErrorMode)
	if err != nil {
		return
	}

	icon.SetTarget(0, 0, float64(w), float64(h))
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	icon.Draw(rasterx.NewDasher(w, h, rasterx.NewScannerGV(w, h, m, m.Bounds())), 1)

	var b bytes.Buffer
	if (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&b, m) != nil {
		return
	}

	a.png[i], a.size[i], a.hash[i] = b.Bytes(), [2]int32{int32(w), int32(h)}, img.HashBytes(b.Bytes())
}

// part appends image i at rect, or the cell uv (texels from the image's corner) of it, when it is ready.
func (a *sceneArt) part(d *Discordo, q []gpu.Quad, i int, rect, cell [4]float32, opacity float64) []gpu.Quad {
	if a.png[i] == nil || d.imgs == nil {
		return q
	}

	h, st := d.imgs.Request(img.Key{Hash: a.hash[i], W: a.size[i][0], H: a.size[i][1]}, img.Bytes(a.png[i]))
	if st != img.Ready {
		return q
	}

	tex, uv, st := d.imgs.Use(h)
	if st != img.Ready {
		return q
	}

	if cell != ([4]float32{}) {
		uv = [4]float32{uv[0] + cell[0], uv[1] + cell[1], uv[0] + cell[2], uv[1] + cell[3]}
	}

	c := fade(0xffffffff, opacity)

	return append(q, gpu.Quad{Rect: rect, UV: uv, Color: [4]uint32{c, c, c, c}, Mode: gpu.ModeRGBA, Tex: tex})
}

// wing appends w at progress p of its beat: the nearest baked turn, squashed to sy about the shoulder.
func (a *sceneArt) wing(d *Discordo, q []gpu.Quad, w *wingArt, p, sy float64, ox, oy, u float32, opacity float64) []gpu.Quad {
	i := int(math.Round(p * (wingAngles - 1)))
	cw, ch := (w.crop[2]-w.crop[0])*wingRes, (w.crop[3]-w.crop[1])*wingRes
	cx, cy := float32(i%wingCols)*cw, float32(i/wingCols)*ch

	pivot := oy + shoulderY*u
	rect := [4]float32{ox + w.crop[0]*u, pivot + (w.crop[1]-shoulderY)*u*float32(sy), ox + w.crop[2]*u, pivot + (w.crop[3]-shoulderY)*u*float32(sy)}

	return a.part(d, q, w.part, rect, [4]float32{cx, cy, cx + cw, cy + ch}, opacity)
}
