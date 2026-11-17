package main

// Shared building blocks of the window: the palette, fonts, boxes, text, avatars, buttons, REST calls.
// Every area uses them; change them only with everyone in mind.

import (
	"context"
	"strings"
	"unicode/utf8"
	"unsafe"

	"gonativeui/app/shell"
	"gonativeui/icon"
	"gonativeui/img"
	"gonativeui/rexui"
	"gonativeui/text"
	"gonativeui/theme"
	"gonativeui/ui"
	"gonativeui/widget/base"
)

// The palette of design/tokens.css (0xRRGGBBAA): the default skin; a theme (theme.go) replaces them.
var (
	blurple     uint32 = 0x5865f2ff
	blurpleSoft uint32 = 0x7983f5ff
	green       uint32 = 0x23a55aff
	cerulean    uint32 = 0x00a8fcff
	fuchsia     uint32 = 0xeb459eff
	ember       uint32 = 0xf0b232ff
	red         uint32 = 0xf23f42ff
	snow        uint32 = 0xf2f3f5ff
	fog         uint32 = 0xb5bac1ff
	greyple     uint32 = 0x949ba4ff
	white       uint32 = 0xffffffff

	surfaceCosmic  uint32 = 0x0e0f2dff
	surfaceRail    uint32 = 0x1e1f22ff
	surfaceSidebar uint32 = 0x2b2d31ff
	surfaceContent uint32 = 0x313338ff
	surfaceRaised  uint32 = 0x383a40ff
	surfaceHover   uint32 = 0x35373cff
	surfaceActive  uint32 = 0x404249ff
	canvas         uint32 = 0x1e1f22ff

	line10 uint32 = 0xffffff1a // border-white/10
	line5  uint32 = 0xffffff0d // border-white/5
)

func rgbaF(c uint32) [4]float32 {
	return [4]float32{float32(c>>24) / 255, float32(c>>16&0xff) / 255, float32(c>>8&0xff) / 255, float32(c&0xff) / 255}
}

// alpha replaces a colour's opacity (0 to 1): Tailwind's "bg-blurple/20".
func alpha(c uint32, a float32) uint32 { return c&^0xff | uint32(a*255+0.5) }

// avatarColors are design/avatar.tsx's, picked by the user ID as it does.
var avatarColors = [...]uint32{blurple, cerulean, fuchsia, ember, green}

func avatarColor(userID string) uint32 {
	sum := 0
	for _, r := range userID {
		sum += int(r)
	}

	return avatarColors[sum%len(avatarColors)]
}

// Widths, in points: the rail (w-[72px]), the sidebar and the member list (w-60).
const (
	railW     = 72
	sidebarW  = 240
	membersW  = 240
	headerH   = 48
	membersAt = 1024 // lg: the member list shows from this window width
)

// font is a font of the window's scale and its line height in pixels.
type font struct {
	key  text.FontKey
	line float32
}

// uiCache holds what follows the window's scale: the fonts, the rich text styles and the icons.
type uiCache struct {
	scale float32
	n     uint64 // the frame's text counter (texts have no state: an order is identity enough)

	caption, s11, s12, s12s, s14, s14m, s14s, s15, s16, s16b, s18, time, date, s18b, s24b, s30b, s20b, display font

	railText    uint32
	railHot     bool
	railRadius  float32
	spans       []ui.Span
	placeholder string

	rich  ui.RichStyles
	icons map[iconKey]icon.ID

	mascot uint64
	first  string  // the first message of the open log, to keep the view still when older ones arrive
	baseH  float32 // the content's height before they arrived
	adjust bool

	members  []Member // the member list's rows, sorted (made when the guild changes)
	friends  []Relationship
	memberOf string

	addFriend ui.TextBuffer
	addResult string
	confirm   bool // the delete dialog
	deleting  *Message
	picker    string // the message whose reaction picker is open
}

type iconKey struct {
	name string
	pt   float32
}

func (d *Discordo) fonts() {
	sc := d.k.Scale()
	if d.ui.scale == sc && d.ui.icons != nil {
		return
	}

	d.ui.scale = sc
	d.ui.icons = map[iconKey]icon.ID{}
	ts := d.k.TextSystem()

	mk := func(pt float32, weight int, tracking float32) font {
		f := ts.Font(text.FontDesc{SizePt: pt, Weight: weight, Scale: sc, Tracking: tracking})
		return font{key: f, line: theme.LineBox(ts.Metrics(f)).H}
	}

	u := &d.ui
	u.caption = mk(12, 700, 0.02) // section-label
	u.s11 = mk(11, 400, 0)
	u.s12 = mk(12, 400, 0)
	u.s12s = mk(12, 600, 0)
	u.s14 = mk(14, 400, 0)
	u.s14m = mk(14, 500, 0)
	u.s14s = mk(14, 600, 0)
	u.s15 = mk(15, 400, 0)
	u.s16 = mk(16, 400, 0)
	u.s16b = mk(16, 700, 0)
	u.s18 = mk(18, 400, 0)
	u.time = mk(10.9, 400, 0)
	u.date = mk(11.2, 600, 0)
	u.s18b = mk(18, 700, 0)
	u.s20b = mk(20, 700, 0)
	u.s24b = mk(24, 700, 0)
	u.s30b = mk(30, 700, 0)

	disp := ts.Font(text.FontDesc{Family: "Arial Black", SizePt: 80, Weight: 800, Scale: sc, Tracking: -0.01})
	u.display = font{key: disp, line: theme.Snap(80 * 0.9 * sc)} // display-crushed: line-height 0.9

	// A message: the text, a mention (bg-blurple/20 text-blurple-soft), "(editada)".
	u.rich = ui.RichStyles{}
	u.rich.Set(0, ui.RichStyle{Font: u.s15.key, Color: fog})
	u.rich.Set(1, ui.RichStyle{Font: mk(15, 500, 0).key, Color: blurpleSoft, BG: alpha(blurple, 0.2), Radius: 3 * sc, PadY: 1 * sc})
	u.rich.Set(2, ui.RichStyle{Font: mk(10.4, 400, 0).key, Color: greyple})
}

func (d *Discordo) px(pt float32) float32 { return theme.Snap(pt * d.ui.scale) }

// Lucide icons, as the web's lucide-react.
func (d *Discordo) icon(name string, pt float32, color uint32) ui.ID {
	return d.k.Icon(d.iconID(name, pt), color)
}

func (d *Discordo) iconID(name string, pt float32) icon.ID {
	k := iconKey{name, pt}
	id, ok := d.ui.icons[k]
	if !ok {
		id = d.k.AddIcon(icon.Lucide(name).Size(pt))
		d.ui.icons[k] = id
	}

	return id
}

// box describes a flex box, Tailwind-style: sizes in pixels or ui sizes, paddings and gaps in points.
type box struct {
	axis           ui.Axis
	w, h           ui.Size
	l, t, r, b     float32
	gap            float32
	bg             uint32
	radius         float32
	cross          ui.Cross
	justify        ui.Justify
	clip, floating bool
}

func (d *Discordo) style(b box) ui.Flags {
	c := d.k.Context()
	var flags ui.Flags

	if b.w != (ui.Size{}) || b.h != (ui.Size{}) {
		w, h := b.w, b.h
		if w == (ui.Size{}) {
			w = ui.Children(1)
		}

		if h == (ui.Size{}) {
			h = ui.Children(1)
		}

		c.NextSize(w, h)
	}

	c.NextAxis(b.axis)
	c.NextPadding(d.px(b.l), d.px(b.t), d.px(b.r), d.px(b.b))
	c.NextGap(d.px(b.gap))
	c.NextCross(b.cross)
	c.NextJustify(b.justify)

	if b.bg != 0 {
		c.NextBG(b.bg)
		flags |= ui.DrawBackground
	}

	if b.radius != 0 {
		c.NextRadius(d.px(b.radius))
	}

	if b.clip {
		flags |= ui.Clip
	}

	if b.floating {
		flags |= base.Floating
	}

	return flags
}

// open builds a box and makes it the parent; close with c.Pop().
func (d *Discordo) begin(label string, b box) ui.ID {
	c := d.k.Context()
	id := c.Box(d.style(b), label)
	c.Push(id)

	return id
}

func (d *Discordo) beginKey(key ui.Key, b box) ui.ID {
	c := d.k.Context()
	id := c.BoxKeyed(d.style(b), key, nil)
	c.Push(id)

	return id
}

// press opens a clickable box; hot says the pointer is over it (to colour it and its content). Close with
// pop, which returns whether it was clicked.
func (d *Discordo) press(key ui.Key, b box, hoverBG uint32, name string) (hot bool) {
	k, c := d.k, d.k.Context()
	v := k.VisualOf(key, base.HoverOn)
	hot = v.Hot > 0.5

	if hot && hoverBG != 0 {
		b.bg = hoverBG
	}

	flags := d.style(b) | k.InteractFlags(base.HoverOn)
	c.NextRole(ui.RoleButton)
	c.NextSemName(name)
	c.NextCursor(ui.CursorPointer)
	c.Push(c.BoxKeyed(flags, key, nil))

	return hot
}

func (d *Discordo) pop() bool {
	c := d.k.Context()
	id := c.Parent()
	c.Pop()

	s := c.Signal(id)

	return s.Clicked || s.Activated
}

func view(s string) []byte { return unsafe.Slice(unsafe.StringData(s), len(s)) }

// text is one line in a font and a colour, cut with "…" where it does not fit.
func (d *Discordo) text(s string, f font, color uint32) ui.ID {
	k, c := d.k, d.k.Context()
	d.ui.n++
	k.ApplySize(ui.TextSize(0).Strict(0), ui.Px(f.line))
	c.NextFont(f.key)
	c.NextTextColor(color)
	c.NextRole(ui.RoleLabel)
	c.NextSemName(s)

	return c.BoxKeyed(ui.DrawText, ui.KeyN(c.ParentKey(), d.ui.n), view(s))
}

// para is text that wraps at the parent's width.
func (d *Discordo) para(s string, f font, color uint32) ui.ID {
	k, c := d.k, d.k.Context()
	d.ui.n++
	k.ApplySize(ui.Pct(1), ui.TextSize(0))
	c.NextFont(f.key)
	c.NextTextColor(color)
	c.NextRole(ui.RoleLabel)
	c.NextSemName(s)

	return c.BoxKeyed(ui.DrawText|ui.WrapText, ui.KeyN(c.ParentKey(), d.ui.n), view(s))
}

// rule is a line across its parent: a border-b or border-t.
func (d *Discordo) rule(color uint32, vertical bool) {
	c := d.k.Context()
	d.ui.n++

	if vertical {
		c.NextSize(ui.Px(1), ui.Pct(1))
	} else {
		c.NextSize(ui.Pct(1), ui.Px(1))
	}

	c.NextBG(color)
	c.BoxKeyed(ui.DrawBackground, ui.KeyN(c.ParentKey(), d.ui.n), nil)
}

func (d *Discordo) spacer() {
	c := d.k.Context()
	d.ui.n++
	c.NextSize(ui.Grow(1), ui.Px(1))
	c.BoxKeyed(0, ui.KeyN(c.ParentKey(), d.ui.n), nil)
}

// avatar is design/avatar.tsx: the picture or the initial on the user's colour, with the presence dot.
func (d *Discordo) avatar(u User, name string, size float32, presence rexui.AvatarPresence, ring uint32) ui.ID {
	px := int32(d.px(size))
	h, _ := d.image(u.AvatarURL, px, px, true)

	initial := "?"
	if r, n := utf8.DecodeRuneInString(strings.TrimSpace(name)); n > 0 {
		initial = strings.ToUpper(string(r))
	}

	return rexui.Avatar(d.k.Kit, name+"###"+u.ID, rexui.AvatarOpts{
		Size: size, Image: h, Color: rexui.OfRGBA(avatarColor(u.ID)), Initials: initial,
		InitialsColor: rexui.OfRGBA(white), InitialsScale: 0.4, InitialsWeight: 700,
		Presence: presence, Backdrop: rexui.OfRGBA(ring),
	})
}

func (d *Discordo) presence(userID string) rexui.AvatarPresence {
	if d.st.Online(userID) {
		return rexui.PresenceOnline
	}

	return rexui.PresenceNone
}

// styleComponents gives the composer the web's look: a rounded raised field without an outline.
func styleComponents() {
	l := &rexui.ComposerLook
	l.Radius, l.BorderW = 8, 0
	l.Fill = rexui.OfRGBA(surfaceRaised)
	l.Text, l.Placeholder = rexui.OfRGBA(fog), rexui.OfRGBA(greyple)
}

func (d *Discordo) mascot(size int32) (img.Handle, img.State) {
	if d.ui.mascot == 0 {
		d.ui.mascot = img.HashBytes(mascot)
	}

	return d.request(d.ui.mascot, mascot, size, size, true)
}

// initialsOf is server-rail.tsx's: the first letter of up to two words.
func initialsOf(name string) string {
	var b strings.Builder
	for _, w := range strings.Fields(name) {
		r, _ := utf8.DecodeRuneInString(w)
		b.WriteRune(r)

		if b.Len() >= 2 || utf8.RuneCountInString(b.String()) == 2 {
			break
		}
	}

	if b.Len() == 0 {
		return "?"
	}

	return strings.ToUpper(b.String())
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}

	return itoa(n) + " " + many
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	var b [20]byte
	i := len(b)
	for ; n > 0; n /= 10 {
		i--
		b[i] = byte('0' + n%10)
	}

	return string(b[i:])
}

// badge is the red count of friend requests.
func (d *Discordo) badge(n int) {
	c := d.k.Context()
	d.begin("badge", box{h: ui.Px(d.px(20)), l: 6, r: 6, radius: 10, bg: red, cross: ui.CrossCenter, justify: ui.JustifyCenter})
	c.NextMinSize(d.px(20), 0)
	d.text(itoa(n), d.ui.s12s, white)
	c.Pop()
}

// ghost is the web's ghost icon-sm button: a 32 pt square that lights on hover.
func (d *Discordo) ghost(iconName, label string, pt float32) bool {
	c := d.k.Context()
	hot := d.press(ui.HashString(c.ParentKey(), label), box{w: ui.Px(d.px(32)), h: ui.Px(d.px(32)), radius: 8, cross: ui.CrossCenter, justify: ui.JustifyCenter}, surfaceHover, label)
	color := uint32(fog)
	if hot {
		color = snow
	}

	d.icon(iconName, pt, color)
	id := c.Parent()
	clicked := d.pop()
	rexui.Tooltip(d.k.Kit, id, label)

	return clicked
}

// openWeb opens a page of the web app for what the native app does not do yet (settings, new servers).
func (d *Discordo) openWeb(path string) {
	go shell.OpenURL(d.client.Server + "/" + path)
}

// iconButton is the ghost icon-xs of the hover bar; it returns the button's box.
func (d *Discordo) iconButton(name, label string, size float32) ui.ID {
	c := d.k.Context()
	hot := d.press(ui.HashString(c.ParentKey(), label), box{w: ui.Px(d.px(size)), h: ui.Px(d.px(size)), radius: 4, cross: ui.CrossCenter, justify: ui.JustifyCenter}, surfaceHover, label)
	color := uint32(fog)
	if hot {
		color = snow
	}

	d.icon(name, 14, color)
	id := c.Parent()
	c.Pop()
	rexui.Tooltip(d.k.Kit, id, label)

	return id
}

func (d *Discordo) clicked(id ui.ID) bool {
	s := d.k.Context().Signal(id)
	return s.Clicked || s.Activated
}

func formatSize(n int64) string {
	switch {
	case n >= 1<<20:
		return itoa(int(n*10>>20)/10) + "," + itoa(int(n*10>>20)%10) + " MB"
	case n >= 1<<10:
		return itoa(int(n>>10)) + " KB"
	}

	return itoa(int(n)) + " B"
}

// secondary is the web's secondary icon-sm button: a raised 32 pt square.
func (d *Discordo) secondary(iconName, label string, color uint32) bool {
	c := d.k.Context()
	d.press(ui.HashString(c.ParentKey(), label), box{w: ui.Px(d.px(32)), h: ui.Px(d.px(32)), radius: 8, bg: surfaceRaised, cross: ui.CrossCenter, justify: ui.JustifyCenter}, surfaceActive, label)
	if color == 0 {
		color = snow
	}

	d.icon(iconName, 16, color)
	id := c.Parent()
	clicked := d.pop()
	rexui.Tooltip(d.k.Kit, id, label)

	return clicked
}

func (d *Discordo) secondaryText(label string) bool {
	c := d.k.Context()
	d.press(ui.HashString(c.ParentKey(), label), box{axis: ui.AxisX, h: ui.Px(d.px(32)), l: 12, r: 12, radius: 8, bg: surfaceRaised, cross: ui.CrossCenter}, surfaceActive, label)
	d.text(label, d.ui.s14m, snow)

	return d.pop()
}

// call is a REST call whose answer is applied on the UI thread; a failure shows in the status line.
func call[T any](d *Discordo, method, path string, body any, done func(*Discordo, T)) {
	client, gen := d.client, d.gen
	d.status = ""

	go func() {
		var out T
		var target any = &out
		if _, empty := any(out).(struct{}); empty {
			target = nil
		}

		err := client.do(context.Background(), method, path, body, target)

		d.post(func(d *Discordo) {
			if d.gen != gen {
				return
			}

			if err != nil {
				d.status = err.Error()
				return
			}

			done(d, out)
		})
	}()
}

// primary is the web's lg primary button across its parent.
func (d *Discordo) primary(label string) bool {
	c := d.k.Context()
	d.press(ui.HashString(c.ParentKey(), label), box{axis: ui.AxisX, w: ui.Pct(1), h: ui.Px(d.px(40)), radius: 8, bg: blurple, gap: 8, cross: ui.CrossCenter, justify: ui.JustifyCenter}, 0x4752c4ff, label)
	d.text(label, d.ui.s14m, white)

	return d.pop()
}

// outline gives the next box a border: NextBorder draws only with DrawBorder.
func outline(c *ui.Context, color uint32, width float32) {
	c.NextBorder(color, width)
	c.NextFlags(ui.DrawBorder)
}
