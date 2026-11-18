package main

// Themes (components/theme-provider.tsx, design/tokens.css): the Discordo palette, five solid skins and the
// animated Neon and Dragão (theme_scene.go), chosen in "Aparência" and kept across launches.

import (
	"path/filepath"
	"testing"

	"gonativeui/app/paths"
	"gonativeui/app/prefs"
	"gonativeui/rexui"
	"gonativeui/theme"
	"gonativeui/theme/rex"
	"gonativeui/ui"
	"gonativeui/widget/base"
)

// palette is what a skin overrides: the package colours of kit.go.
type palette struct {
	blurple, blurpleSoft, green, cerulean, fuchsia, ember, red, snow, fog, greyple uint32
	cosmic, rail, sidebar, content, raised, hover, active, canvas, line10, line5   uint32
}

// apply makes p the window's colours; everything reads them each frame.
func (p *palette) apply() {
	blurple, blurpleSoft, green, cerulean, fuchsia = p.blurple, p.blurpleSoft, p.green, p.cerulean, p.fuchsia
	ember, red, snow, fog, greyple = p.ember, p.red, p.snow, p.fog, p.greyple

	surfaceCosmic, surfaceRail, surfaceSidebar, surfaceContent = p.cosmic, p.rail, p.sidebar, p.content
	surfaceRaised, surfaceHover, surfaceActive, canvas = p.raised, p.hover, p.active, p.canvas
	line10, line5 = p.line10, p.line5

	avatarColors = [...]uint32{blurple, cerulean, fuchsia, ember, green}
	styleComponents()
}

// basePalette is kit.go's defaults: the Discordo skin, and the start of every other.
var basePalette = palette{
	blurple, blurpleSoft, green, cerulean, fuchsia, ember, red, snow, fog, greyple,
	surfaceCosmic, surfaceRail, surfaceSidebar, surfaceContent, surfaceRaised, surfaceHover, surfaceActive, canvas, line10, line5,
}

func over(f func(p *palette)) palette {
	p := basePalette
	f(&p)

	return p
}

type skin struct {
	id, label, desc string
	light           bool // Papel: the kit's widgets follow in their light mode
	animated        bool // a scene (theme_scene.go) behind translucent panes
	p               palette
}

// skins are SKINS of theme-provider.tsx, but Liquid Glass (it needs blurred panes).
var skins = []skin{
	{id: "default", label: "Discordo", desc: "O visual clássico, escuro.", p: basePalette},

	{id: "midnight", label: "Meia-noite", desc: "Preto puro, bom para telas OLED.", p: over(func(p *palette) {
		p.cosmic, p.rail, p.sidebar, p.content = 0x000000ff, 0x000000ff, 0x0a0a0cff, 0x111114ff
		p.raised, p.hover, p.active, p.canvas = 0x1a1a1fff, 0x17171bff, 0x222228ff, 0x000000ff
	})},

	{id: "forest", label: "Floresta", desc: "Verdes escuros e calmos.", p: over(func(p *palette) {
		p.blurple, p.blurpleSoft, p.snow, p.fog, p.greyple = 0x2f9e6eff, 0x4cc08bff, 0xeef5f0ff, 0xb3c4baff, 0x8fa398ff
		p.cosmic, p.rail, p.sidebar, p.content = 0x0b1a14ff, 0x0f1a15ff, 0x15231cff, 0x1a2b23ff
		p.raised, p.hover, p.active, p.canvas = 0x22362cff, 0x1f3229ff, 0x2a4236ff, 0x0f1a15ff
	})},

	{id: "ocean", label: "Oceano", desc: "Azul-marinho com destaque ciano.", p: over(func(p *palette) {
		p.blurple, p.blurpleSoft, p.snow, p.fog, p.greyple = 0x1e9bd7ff, 0x4fb6e8ff, 0xeef4faff, 0xb0c2d3ff, 0x8a9db0ff
		p.cosmic, p.rail, p.sidebar, p.content = 0x08131fff, 0x0b1724ff, 0x10202fff, 0x142636ff
		p.raised, p.hover, p.active, p.canvas = 0x1b3043ff, 0x182b3cff, 0x213a50ff, 0x0b1724ff
	})},

	{id: "dusk", label: "Crepúsculo", desc: "Roxo profundo com destaque rosa.", p: over(func(p *palette) {
		p.blurple, p.blurpleSoft, p.snow, p.fog, p.greyple = 0xc75a9bff, 0xe08bbdff, 0xf6eef7ff, 0xc9b6ccff, 0xa08fa4ff
		p.cosmic, p.rail, p.sidebar, p.content = 0x1a0f1fff, 0x1b1220ff, 0x241829ff, 0x2b1d31ff
		p.raised, p.hover, p.active, p.canvas = 0x36263dff, 0x312236ff, 0x3f2c47ff, 0x1b1220ff
	})},

	{id: "paper", label: "Papel", desc: "Claro e limpo, para ambientes iluminados.", light: true, p: over(func(p *palette) {
		p.blurpleSoft, p.green, p.cerulean, p.red = 0x4752c4ff, 0x188a48ff, 0x0071bcff, 0xd92b2eff
		p.snow, p.fog, p.greyple = 0x1f2328ff, 0x4e5058ff, 0x6d6f78ff
		p.cosmic, p.rail, p.sidebar, p.content = 0xe3e5e8ff, 0xe3e5e8ff, 0xf2f3f5ff, 0xffffffff
		p.raised, p.hover, p.active, p.canvas = 0xebedefff, 0xe8eaedff, 0xdfe1e5ff, 0xe3e5e8ff
		p.line10, p.line5 = 0x0000001a, 0x00000014
	})},

	// The animated skins' panes are translucent, as the web's, so the scene shows through.
	{id: "neon", label: "Neon", desc: "Cidade à noite: rosa, ciano e grade luminosa.", animated: true, p: over(func(p *palette) {
		p.blurple, p.blurpleSoft, p.fuchsia, p.cerulean, p.ember = 0xff2a6dff, 0x05d9e8ff, 0xff2a6dff, 0x05d9e8ff, 0xf3e600ff
		p.snow, p.fog, p.greyple = 0xe8fbffff, 0x9fd8e0ff, 0x6fa3adff
		p.cosmic, p.rail, p.sidebar, p.content = 0x05040ce6, 0x06050ee6, 0x0a0916d9, 0x0c0b1ac7
		p.raised, p.hover, p.active, p.canvas = 0x1a1834e6, 0x05d9e814, 0xff2a6d38, 0x05040cff
		p.line10, p.line5 = 0x05d9e838, 0x05d9e829
	})},

	{id: "dragon", label: "Dragão", desc: "Um dragão animado voando entre brasas.", animated: true, p: over(func(p *palette) {
		p.blurple, p.blurpleSoft, p.snow, p.fog, p.greyple = 0xe2552bff, 0xff8a5cff, 0xfbefe8ff, 0xd6b8abff, 0xa88f84ff
		p.cosmic, p.rail, p.sidebar, p.content = 0x0e0606d9, 0x100707d9, 0x180a0ac7, 0x140909a8
		p.raised, p.hover, p.active, p.canvas = 0x301412d9, 0xff785014, 0xe2552b40, 0x0b0505ff
		p.line10, p.line5 = 0xff8c641f, 0xff8c641a
	})},
}

// themePrefs is what theme.json keeps.
type themePrefs struct {
	Skin   string
	Static bool // "Tema estático": the animated skins hold still

	WorldSkin string // the character worn in every world, "" for my own
}

var (
	themeStore *prefs.Store[themePrefs] // nil in tests and when there is no config directory
	skinNow    int
	themeStill bool
	kitAccent  uint32 // the accent the kit's theme was made with
)

// The saved skin is painted before main.go's Init and config read the palette (its accent, the clear colour).
func init() {
	if testing.Testing() {
		return
	}

	if dir, err := paths.Config("br.com.discordo.desktop"); err == nil {
		if dir, err = paths.Ensure(dir); err == nil {
			openThemePrefs(filepath.Join(dir, "theme.json"))
		}
	}

	kitAccent = blurple
}

// openThemePrefs reads the choice at path and paints it; a broken file leaves the defaults.
func openThemePrefs(path string) {
	s, _ := prefs.Open(path, themePrefs{Skin: "default"}, prefs.Options{})
	themeStore = s

	v := s.Get()
	themeStill = v.Static
	skinNow = skinIndex(v.Skin)
	skins[skinNow].p.apply()
}

func skinIndex(id string) int {
	for i, s := range skins {
		if s.id == id {
			return i
		}
	}

	return 0
}

// setSkin paints skins[i] and saves the choice.
func setSkin(i int) {
	skinNow = i
	skins[i].p.apply()

	if themeStore != nil {
		themeStore.Update(func(v *themePrefs) { v.Skin = skins[i].id })
	}
}

func setStill(still bool) {
	themeStill = still

	if themeStore != nil {
		themeStore.Update(func(v *themePrefs) { v.Static = still })
	}
}

// syncKit gives the kit's own widgets (switches, spinners, dialogs) the skin's accent and mode.
func (d *Discordo) syncKit() {
	if kitAccent != blurple {
		kitAccent = blurple
		th := rex.Theme()
		th.SetAccent(blurple, blurple)
		d.k.SetTheme(th)
	}

	want := theme.ForceDark
	if skins[skinNow].light {
		want = theme.ForceLight
	}

	if d.k.Preference() != want {
		d.k.SetPreference(want)
	}
}

// appearanceSection is the user settings' "Aparência" (components/appearance-settings.tsx): the skins, each
// with a tiny workspace in its colours, and "Tema estático". Its colour mode and transparency switches are
// Liquid Glass's only, so they are not here.
func (d *Discordo) appearanceSection() {
	d.syncKit()
	c := d.k.Context()

	d.begin("appearance", box{axis: ui.AxisY, w: ui.Pct(1), gap: 20})

	d.begin("skins", box{axis: ui.AxisY, w: ui.Pct(1), gap: 8})
	d.text("TEMA", d.ui.caption, greyple)

	const cols = 3
	for row := 0; row < len(skins); row += cols {
		d.begin("row", box{axis: ui.AxisX, w: ui.Pct(1), gap: 12})

		for i := row; i < row+cols; i++ {
			if i >= len(skins) {
				d.begin("filler", box{w: ui.Grow(1)}) // keeps the last row's cards as wide as the others
				c.Pop()

				continue
			}

			d.skinCard(i)
		}

		c.Pop()
	}

	c.Pop()

	d.begin("still", box{axis: ui.AxisX, w: ui.Pct(1), gap: 16, cross: ui.CrossCenter})
	d.begin("label", box{axis: ui.AxisY, w: ui.Grow(1), gap: 2})
	d.text("Tema estático", d.ui.s14s, snow)
	d.para(`Para o fundo animado do Neon e do Dragão. Também segue o "reduzir movimento" do sistema.`, d.ui.s12, greyple)
	c.Pop()

	still := themeStill
	if rexui.Switch(d.k.Kit, "##Tema estático", &still, rexui.SwitchOpts{}).Changed {
		setStill(still)
	}

	c.Pop()
	c.Pop()
}

// skinCard is one radio of the grid: the preview, the name (with a check when chosen) and the line.
func (d *Discordo) skinCard(i int) {
	c := d.k.Context()
	s := &skins[i]
	chosen := i == skinNow

	// press(), with the ring: ring-1 ring-border, ring-2 ring-blurple when chosen.
	key := ui.HashString(c.ParentKey(), s.id)
	switch {
	case chosen:
		c.NextBorder(blurple, d.px(2))
	case d.k.VisualOf(key, base.HoverOn).Hot > 0.5:
		c.NextBorder(alpha(fog, 0.4), d.px(1))
	default:
		c.NextBorder(line10, d.px(1))
	}

	flags := d.style(box{axis: ui.AxisY, w: ui.Grow(1), radius: 12, l: 6, r: 6, t: 6, b: 6, gap: 2})
	c.NextRole(ui.RoleButton)
	c.NextSemName(s.label)
	c.NextCursor(ui.CursorPointer)
	c.Push(c.BoxKeyed(flags|ui.DrawBorder|d.k.InteractFlags(base.HoverOn), key, nil))

	d.skinPreview(&s.p)

	d.begin("name", box{axis: ui.AxisX, gap: 6, l: 4, t: 6, cross: ui.CrossCenter})
	d.text(s.label, d.ui.s14s, snow)

	if chosen {
		d.icon("check", 16, blurpleSoft)
	}

	c.Pop()

	d.begin("desc", box{axis: ui.AxisY, w: ui.Pct(1), l: 4, r: 4, b: 4})
	d.para(s.desc, d.ui.s12, greyple)
	c.Pop()

	if d.pop() && !chosen {
		setSkin(i)
		d.syncKit()
	}
}

// skinPreview is SkinPreview: the rail, the sidebar and a chat, painted with p rather than the window's colours.
func (d *Discordo) skinPreview(p *palette) {
	c := d.k.Context()
	line := func(w ui.Size, color uint32) {
		d.begin("line", box{w: w, h: ui.Px(d.px(4)), bg: color, radius: 2})
		c.Pop()
	}

	dot := func(pt float32, color uint32) {
		d.begin("dot", box{w: ui.Px(d.px(pt)), h: ui.Px(d.px(pt)), bg: color, radius: pt / 2})
		c.Pop()
	}

	d.begin("preview", box{axis: ui.AxisX, w: ui.Pct(1), h: ui.Px(d.px(96)), bg: p.canvas, radius: 8, clip: true})

	d.begin("rail", box{axis: ui.AxisY, w: ui.Px(d.px(24)), h: ui.Pct(1), bg: p.rail, t: 6, gap: 4, cross: ui.CrossCenter})
	dot(14, p.blurple)
	dot(14, p.raised)
	c.Pop()

	d.begin("sidebar", box{axis: ui.AxisY, w: ui.Px(d.px(64)), h: ui.Pct(1), bg: p.sidebar, l: 8, r: 8, t: 8, gap: 6})
	line(ui.Pct(0.75), alpha(p.greyple, 0.7))
	line(ui.Pct(1), p.active)
	line(ui.Pct(0.66), alpha(p.greyple, 0.7))
	c.Pop()

	d.begin("chat", box{axis: ui.AxisY, w: ui.Grow(1), h: ui.Pct(1), bg: p.content, l: 8, r: 8, t: 8, b: 8, gap: 6, justify: ui.JustifyEnd})

	for _, m := range [...]struct {
		avatar, text uint32
		w            float32
	}{{p.fuchsia, p.snow, 0.5}, {p.cerulean, alpha(p.fog, 0.8), 0.66}} {
		d.begin("message", box{axis: ui.AxisX, w: ui.Pct(1), gap: 6, cross: ui.CrossCenter})
		dot(12, m.avatar)
		line(ui.Pct(m.w), m.text)
		c.Pop()
	}

	d.begin("composer", box{w: ui.Pct(1), h: ui.Px(d.px(12)), bg: p.raised, radius: 2})
	c.Pop()
	c.Pop()

	c.Pop()
}

// themeBackdrop draws the skin's scene behind the workspace (nothing for a solid skin).
func (d *Discordo) themeBackdrop(W, H float32) {
	d.syncKit()

	switch skins[skinNow].id {
	case "neon":
		d.neonScene(W, H)
	case "dragon":
		d.dragonScene(W, H)
	}
}
