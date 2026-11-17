package main

// The window, drawn to match the web app (apps/web): its palette (design/tokens.css), its sizes in CSS
// pixels (taken as points here), and the same places for everything. Each part names the web component it is.

import (
	"context"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
	"unsafe"

	"gonativeui/app/shell"
	"gonativeui/icon"
	"gonativeui/img"
	"gonativeui/rexui"
	"gonativeui/text"
	"gonativeui/theme"
	"gonativeui/ui"
	"gonativeui/widget"
	"gonativeui/widget/base"
)

// The palette of design/tokens.css (0xRRGGBBAA).
const (
	blurple     = 0x5865f2ff
	blurpleSoft = 0x7983f5ff
	green       = 0x23a55aff
	cerulean    = 0x00a8fcff
	fuchsia     = 0xeb459eff
	ember       = 0xf0b232ff
	red         = 0xf23f42ff
	snow        = 0xf2f3f5ff
	fog         = 0xb5bac1ff
	greyple     = 0x949ba4ff
	white       = 0xffffffff

	surfaceCosmic  = 0x0e0f2dff
	surfaceRail    = 0x1e1f22ff
	surfaceSidebar = 0x2b2d31ff
	surfaceContent = 0x313338ff
	surfaceRaised  = 0x383a40ff
	surfaceHover   = 0x35373cff
	surfaceActive  = 0x404249ff
	canvas         = 0x1e1f22ff

	line10 = 0xffffff1a // border-white/10
	line5  = 0xffffff0d // border-white/5
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

func (d *Discordo) Build(c *ui.Context) {
	w, h := c.Size()
	if w <= 0 || h <= 0 || c.Scale() <= 0 {
		return
	}

	for drained := false; !drained; {
		select {
		case f := <-d.inbox:
			f(d)
		default:
			drained = true
		}
	}

	if d.imgs != nil && d.imgs.Poll() {
		c.RequestFrame()
	}

	k := d.k
	k.Begin(c)
	d.fonts()
	d.ui.n = 0

	if k.Command(cmdSignOut) && d.phase == phaseOnline {
		d.signOut("")
	}

	W, H := float32(w), float32(h)

	// Where the system draws no menu bar (Windows, Linux) the window's first strip is ours.
	top := float32(0)
	if !c.NativeMenus() {
		top = theme.Snap(k.Theme().Px.ControlH)
		theme.At(c, 0, 0, W, top)
		c.NextBG(surfaceRail)
		c.Push(c.Box(base.Floating|ui.DrawBackground, "menubar"))
		k.MenuBar()
		c.Pop()
	}

	theme.At(c, 0, top, W, H-top)
	c.Push(c.Box(base.Floating, "window"))

	if d.phase == phaseOnline && d.ready {
		d.workspace(W, H-top)
	} else {
		d.login(W, H-top)
	}

	c.Pop()
	d.deleteDialog()
	k.End()
}

// workspace is features/shell/workspace.tsx: rail, sidebar (with the user dock), main, members.
func (d *Discordo) workspace(W, H float32) {
	c := d.k.Context()
	rw, sw := d.px(railW), d.px(sidebarW)

	theme.At(c, 0, 0, rw, H)
	d.begin("rail", box{axis: ui.AxisY, bg: surfaceRail, floating: true, t: 12, b: 12, gap: 8, cross: ui.CrossCenter, clip: true})
	d.rail()
	c.Pop()

	theme.At(c, rw, 0, sw, H)
	d.begin("sidebar", box{axis: ui.AxisY, bg: surfaceSidebar, floating: true, clip: true})
	if g := d.st.Guild(d.guild); g != nil {
		d.guildSidebar(g, H)
	} else {
		d.homeSidebar(H)
	}
	c.Pop()

	// border-r border-white/10
	theme.At(c, rw+sw-1, 0, 1, H)
	c.NextBG(line10)
	c.Box(base.Floating|ui.DrawBackground, "sidebar edge")

	mainX := rw + sw
	mainW := W - mainX
	g := d.st.Guild(d.guild)
	var ch *Channel
	if g != nil {
		ch = g.Channel(d.channel)
	}

	showMembers := ch != nil && ch.Type == ChannelText && W >= d.px(membersAt)
	if showMembers {
		mainW -= d.px(membersW)
	}

	theme.At(c, mainX, 0, mainW, H)
	d.begin("main", box{axis: ui.AxisY, bg: surfaceContent, floating: true, clip: true})

	switch {
	case g != nil && ch == nil:
		d.empty("Selecione um canal", "Escolha um canal à esquerda para começar")
	case ch != nil && ch.Type == ChannelVoice:
		d.empty("Canal de voz", "Chamadas de voz e vídeo ainda não estão no app nativo. Use o Discordo na web para entrar.")
	case ch != nil:
		d.chat(ch.Name, ch.Topic, true, ch.Permissions, mainW, H)
	default:
		if dm := d.st.DM(d.channel); dm != nil {
			d.chat(d.st.DMName(*dm), "", false, permSend|permReact, mainW, H)
		} else {
			d.friendsView()
		}
	}

	c.Pop()

	if showMembers {
		theme.At(c, W-d.px(membersW), 0, d.px(membersW), H)
		d.begin("members", box{axis: ui.AxisX, bg: surfaceSidebar, floating: true})
		d.rule(line5, true)
		d.memberList(g, H)
		c.Pop()
	}
}

// empty is GuildMain's "Selecione um canal": a title and a line, centred.
func (d *Discordo) empty(title, line string) {
	d.begin("empty", box{axis: ui.AxisY, w: ui.Pct(1), h: ui.Pct(1), cross: ui.CrossCenter, justify: ui.JustifyCenter, gap: 8, l: 32, r: 32})
	d.text(title, d.ui.s24b, snow)
	d.text(line, d.ui.s16, greyple)
	d.k.Context().Pop()
}

// rail is features/spaces/server-rail.tsx.
func (d *Discordo) rail() {
	c := d.k.Context()

	if d.railButton("home", "Inicial", d.guild == "", func(size int32) {
		h, _ := d.mascot(size)
		c.NextSize(ui.Pct(1), ui.Pct(1))
		d.k.Image(1, h, ui.FitFill, 0, "")
	}) {
		d.open("", d.friendOrLastDM())
	}

	if len(d.st.Guilds) > 0 {
		c.NextSize(ui.Px(d.px(32)), ui.Px(d.px(2)))
		c.NextBG(line10)
		c.NextRadius(d.px(1))
		c.Box(ui.DrawBackground, "rail rule")
	}

	for i := range d.st.Guilds {
		g := &d.st.Guilds[i]

		if d.railButton(g.ID, g.Name, g.ID == d.guild, func(size int32) {
			if h, st := d.image(g.IconURL, size, size, true); st == img.Ready {
				c.NextSize(ui.Pct(1), ui.Pct(1))
				d.k.Image(2, h, ui.FitFill, 0, "")
				return
			}

			d.text(initialsOf(g.Name), d.ui.s14s, d.ui.railText)
		}) {
			d.open(g.ID, "")
		}
	}

	if d.railButton("add", "Adicionar um servidor", false, func(int32) {
		d.icon("plus", 20, d.ui.railTextOr(green))
	}) {
		d.openWeb("")
	}
}

// friendOrLastDM: home opens on the friends, as the web's.
func (d *Discordo) friendOrLastDM() string { return "" }

// railButton is RailButton: a 48 pt tile that rounds less and turns blurple when active or hovered, with
// the white pill at the rail's edge.
func (d *Discordo) railButton(name, label string, active bool, content func(size int32)) bool {
	c := d.k.Context()
	d.begin(name, box{axis: ui.AxisX, w: ui.Pct(1), h: ui.Px(d.px(48)), justify: ui.JustifyCenter})

	key := ui.HashString(c.ParentKey(), "button")
	hot := d.k.VisualOf(key, base.HoverOn).Hot > 0.5

	// The pill: h-10 when active, h-5 on hover.
	if active || hot {
		ph := float32(40)
		if !active {
			ph = 20
		}

		c.NextAnchor(ui.AnchorLeft, 0, 0)
		c.NextSize(ui.Px(d.px(4)), ui.Px(d.px(ph)))
		c.NextBG(white)
		c.NextRadii(0, d.px(4), d.px(4), 0)
		c.Box(ui.DrawBackground, "pill")
	}

	bg, radius := uint32(surfaceSidebar), float32(16)
	d.ui.railText = fog
	if active || hot {
		bg, radius = blurple, 12
		d.ui.railText = white
	}

	d.ui.railHot = active || hot
	d.press(key, box{w: ui.Px(d.px(48)), h: ui.Px(d.px(48)), bg: bg, radius: radius, clip: true, cross: ui.CrossCenter, justify: ui.JustifyCenter}, 0, label)
	content(int32(d.px(48)))
	id := c.Parent()
	clicked := d.pop()
	rexui.Tooltip(d.k.Kit, id, label)

	c.Pop()

	return clicked
}

func (u *uiCache) railTextOr(idle uint32) uint32 {
	if u.railHot {
		return white
	}

	return idle
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

// guildSidebar is features/spaces/guild-sidebar.tsx: the header with the guild's name, then the channels.
func (d *Discordo) guildSidebar(g *Guild, H float32) {
	k, c := d.k, d.k.Context()

	hot := d.press(ui.HashString(c.ParentKey(), "guild header"), box{axis: ui.AxisX, w: ui.Pct(1), h: ui.Px(d.px(headerH)), l: 16, r: 16, gap: 8, cross: ui.CrossCenter}, surfaceHover, g.Name)
	_ = hot
	d.text(g.Name, d.ui.s16b, snow)
	d.spacer()
	d.icon("chevron-down", 16, fog)
	if d.pop() {
		d.openWeb("")
	}

	d.rule(line10, false)

	k.NextSize(ui.Pct(1), ui.Grow(1))
	v := k.BeginScroll("channels", &d.sideScroll, base.ScrollVertical)
	d.begin("list", box{axis: ui.AxisY, w: ui.Pct(1), l: 8, r: 8, t: 12, b: 12, gap: 16})

	sections := [...]struct {
		typ   int
		label string
		icon  string
	}{{ChannelText, "CANAIS DE TEXTO", "hash"}, {ChannelVoice, "CANAIS DE VOZ", "volume-2"}}

	canManage := g.Permissions&permManage != 0

	for _, s := range sections {
		any := slices.ContainsFunc(g.Channels, func(ch Channel) bool { return ch.Type == s.typ })
		if !any && !canManage {
			continue
		}

		d.begin(s.label, box{axis: ui.AxisY, w: ui.Pct(1), gap: 2})

		d.begin("label", box{axis: ui.AxisX, w: ui.Pct(1), l: 6, r: 6, b: 2, cross: ui.CrossCenter})
		d.text(s.label, d.ui.caption, alpha(greyple, 0.8))
		c.Pop()

		if !any {
			d.begin("none", box{l: 6, t: 4, b: 4})
			d.text("Nenhum canal", d.ui.s12, alpha(greyple, 0.6))
			c.Pop()
		}

		for i := range g.Channels {
			ch := &g.Channels[i]
			if ch.Type != s.typ {
				continue
			}

			active := ch.ID == d.channel
			bg := uint32(0)
			if active {
				bg = surfaceActive
			}

			hot := d.press(ui.HashString(c.ParentKey(), ch.ID), box{axis: ui.AxisX, w: ui.Pct(1), l: 8, r: 8, t: 6, b: 6, gap: 8, radius: 6, bg: bg, cross: ui.CrossCenter}, surfaceHover, ch.Name)
			color := uint32(greyple)
			if active || hot {
				color = snow
			}

			d.icon(s.icon, 18, color)
			d.text(ch.Name, d.ui.s14, color)

			if d.pop() {
				d.open(g.ID, ch.ID)
			}

			if ch.Type == ChannelVoice {
				d.voiceOccupants(g, ch.ID)
			}
		}

		c.Pop()
	}

	c.Pop()
	k.CloseScrollContent(&v)
	rexui.ScrollBars(k.Kit, &v)
	k.CloseScroll(&v)

	d.userDock()
}

// voiceOccupants is VoiceOccupants: who is in a voice channel, under it.
func (d *Discordo) voiceOccupants(g *Guild, channelID string) {
	callers := d.st.Callers(channelID)
	if len(callers) == 0 {
		return
	}

	c := d.k.Context()
	d.begin(channelID+" callers", box{axis: ui.AxisY, w: ui.Pct(1), l: 28, gap: 2})

	for _, v := range callers {
		name := "Alguém"
		var u User
		if i := slices.IndexFunc(g.Members, func(m Member) bool { return m.User.ID == v.UserID }); i >= 0 {
			u, name = g.Members[i].User, g.Members[i].Name()
		}

		d.begin(v.UserID, box{axis: ui.AxisX, w: ui.Pct(1), l: 6, r: 6, t: 4, b: 4, gap: 8, cross: ui.CrossCenter})
		d.avatar(u, name, 32, rexui.PresenceNone, surfaceSidebar)
		d.text(name, d.ui.s12, greyple)
		d.spacer()

		if slices.Contains(v.Tracks, "screen") {
			d.begin("live", box{bg: red, radius: 4, l: 4, r: 4})
			d.text("AO VIVO", d.ui.caption, white)
			c.Pop()
		}

		if v.Muted {
			d.icon("mic-off", 14, red)
		}

		if v.Deafened {
			d.icon("headphone-off", 14, red)
		}

		c.Pop()
	}

	c.Pop()
}

// homeSidebar is features/social/home-sidebar.tsx: Amigos, then the DMs.
func (d *Discordo) homeSidebar(H float32) {
	k, c := d.k, d.k.Context()

	d.begin("home nav", box{axis: ui.AxisY, w: ui.Pct(1), l: 8, r: 8, t: 8, b: 8})
	active := d.channel == ""
	bg := uint32(0)
	if active {
		bg = surfaceActive
	}

	hot := d.press(ui.HashString(c.ParentKey(), "friends"), box{axis: ui.AxisX, w: ui.Pct(1), h: ui.Px(d.px(40)), l: 12, r: 12, gap: 12, radius: 8, bg: bg, cross: ui.CrossCenter}, surfaceHover, "Amigos")
	color := uint32(fog)
	if active || hot {
		color = snow
	}

	d.icon("users", 20, color)
	d.text("Amigos", d.ui.s14s, color)
	d.spacer()

	if n := d.incoming(); n > 0 {
		d.badge(n)
	}

	if d.pop() {
		d.open("", "")
	}

	c.Pop()

	d.begin("dm header", box{axis: ui.AxisX, w: ui.Pct(1), l: 16, r: 16, t: 8, b: 8, cross: ui.CrossCenter})
	d.text("MENSAGENS DIRETAS", d.ui.caption, greyple)
	c.Pop()

	k.NextSize(ui.Pct(1), ui.Grow(1))
	v := k.BeginScroll("dms", &d.sideScroll, base.ScrollVertical)
	d.begin("dm list", box{axis: ui.AxisY, w: ui.Pct(1), l: 8, r: 8, b: 12, gap: 2})

	for i := range d.st.DMs {
		dm := &d.st.DMs[i]
		active := dm.ID == d.channel
		bg := uint32(0)
		if active {
			bg = surfaceActive
		}

		hot := d.press(ui.HashString(c.ParentKey(), dm.ID), box{axis: ui.AxisX, w: ui.Pct(1), h: ui.Px(d.px(44)), l: 8, r: 8, gap: 10, radius: 8, bg: bg, cross: ui.CrossCenter}, surfaceHover, d.st.DMName(*dm))
		color := uint32(greyple)
		ring := uint32(surfaceSidebar)
		switch {
		case active:
			color, ring = snow, surfaceActive
		case hot:
			color, ring = fog, surfaceHover
		}

		others := d.st.Others(*dm)
		if dm.Type == ChannelDM && len(others) > 0 {
			d.avatar(others[0], others[0].Name(), 32, d.presence(others[0].ID), ring)
		} else {
			d.begin("group", box{w: ui.Px(d.px(32)), h: ui.Px(d.px(32)), radius: 16, bg: blurple, cross: ui.CrossCenter, justify: ui.JustifyCenter})
			d.icon("users", 16, white)
			c.Pop()
		}

		d.begin("name", box{axis: ui.AxisY, w: ui.Grow(1)})
		d.text(d.st.DMName(*dm), d.ui.s14m, color)
		if dm.Type == ChannelGroup {
			d.text(plural(len(dm.Recipients), "membro", "membros"), d.ui.s12, greyple)
		}
		c.Pop()

		if d.pop() {
			d.open("", dm.ID)
		}
	}

	if len(d.st.DMs) == 0 {
		d.begin("none", box{axis: ui.AxisY, w: ui.Pct(1), t: 20, b: 20, cross: ui.CrossCenter})
		d.text("Nenhuma conversa ainda.", d.ui.s12, greyple)
		c.Pop()
	}

	c.Pop()
	k.CloseScrollContent(&v)
	rexui.ScrollBars(k.Kit, &v)
	k.CloseScroll(&v)

	d.userDock()
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

func (d *Discordo) incoming() int {
	n := 0
	for _, r := range d.st.Relationships {
		if r.Status == 2 {
			n++
		}
	}

	return n
}

// badge is the red count of friend requests.
func (d *Discordo) badge(n int) {
	c := d.k.Context()
	d.begin("badge", box{h: ui.Px(d.px(20)), l: 6, r: 6, radius: 10, bg: red, cross: ui.CrossCenter, justify: ui.JustifyCenter})
	c.NextMinSize(d.px(20), 0)
	d.text(itoa(n), d.ui.s12s, white)
	c.Pop()
}

// userDock is features/shell/user-dock.tsx with features/identity/user-panel.tsx.
func (d *Discordo) userDock() {
	c := d.k.Context()
	d.rule(line10, false)
	d.begin("dock", box{axis: ui.AxisX, w: ui.Pct(1), h: ui.Px(d.px(56)), l: 8, r: 8, gap: 8, bg: surfaceRail, cross: ui.CrossCenter})

	me := d.st.Me
	d.avatar(me, me.Name(), 32, rexui.PresenceOnline, surfaceRail)

	d.begin("who", box{axis: ui.AxisY, w: ui.Grow(1)})
	d.text(me.Name(), d.ui.s12s, snow)
	d.text("@"+me.Username, d.ui.s11, greyple)
	c.Pop()

	if d.ghost("settings", "Configurações", 16) {
		d.openWeb("")
	}

	if d.ghost("log-out", "Sair", 16) {
		d.signOut("")
	}

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

// chat is features/chat/channel-chat.tsx: the header, the transcript, the composer.
func (d *Discordo) chat(title, topic string, guild bool, perms int64, w, H float32) {
	k, c := d.k, d.k.Context()

	d.begin("header", box{axis: ui.AxisX, w: ui.Pct(1), h: ui.Px(d.px(headerH)), l: 16, r: 16, gap: 8, cross: ui.CrossCenter})
	if guild {
		d.icon("hash", 20, greyple)
	}

	d.text(title, d.ui.s16b, snow)

	if topic != "" {
		d.begin("divider", box{l: 8, r: 8, h: ui.Px(d.px(24))})
		d.rule(line10, true)
		c.Pop()
		d.text(topic, d.ui.s12, greyple)
	}

	c.Pop()
	d.rule(line5, false)

	k.NextSize(ui.Pct(1), ui.Grow(1))
	v := k.BeginScroll("transcript", &d.logScroll, base.ScrollVertical)
	d.transcript(v, title, guild, perms)
	k.CloseScrollContent(&v)
	rexui.ScrollBars(k.Kit, &v)
	k.CloseScroll(&v)

	d.composerBar(title, guild, perms)
}

// transcript is features/chat/transcript.tsx: the start of the channel (or a spinner), the days, the messages.
func (d *Discordo) transcript(v base.ScrollView, title string, guild bool, perms int64) {
	c := d.k.Context()
	log := d.st.Logs[d.channel]

	_, sy := c.Scroll(v.Inner)
	content, view := c.ScrollExtent(v.Inner)
	d.atEnd = float32(sy) >= content[1]-view[1]-d.px(80)

	// Near the top: the page before (NEAR * 3 in the web).
	if log != nil && !log.Complete && sy < float64(d.px(240)) {
		d.loadPage(d.channel)
	}

	// Older messages arrived above: keep what was in view where it was.
	first := ""
	if log != nil && len(log.List) > 0 {
		first = log.List[0].ID
	}

	switch {
	case first != d.ui.first:
		d.ui.adjust = d.ui.first != "" && d.pin == 0
		d.ui.first, d.ui.baseH = first, content[1]
		c.RequestFrame()
	case d.ui.adjust && content[1] != d.ui.baseH:
		c.SetScroll(v.Inner, 0, sy+float64(content[1]-d.ui.baseH))
		d.ui.adjust = false
	}

	if d.pin > 0 {
		c.SetScroll(v.Inner, 0, 1e9) // the layout clamps it to the end
		d.pin--
		c.RequestFrame()
	}

	// min-h-full justify-end: a short channel sits at the bottom.
	c.NextMinSize(0, view[1])
	// The messages run edge to edge (li -mx-4 px-4) so their hover and mention colours do; the rest is inset.
	d.begin("log", box{axis: ui.AxisY, w: ui.Pct(1), t: 16, b: 20, justify: ui.JustifyEnd})

	if log == nil || !log.Complete {
		d.begin("loading", box{axis: ui.AxisY, w: ui.Pct(1), t: 24, b: 24, l: 16, r: 16, cross: ui.CrossCenter})
		rexui.Spinner(d.k.Kit, "Carregando", rexui.SpinnerOpts{})
		c.Pop()
	} else {
		d.welcome(title, guild)
	}

	if log != nil {
		var colors map[string]int
		if g := d.st.Guild(d.guild); g != nil {
			colors = g.RoleColors()
		}

		for i := range log.List {
			m := &log.List[i]
			var prev *Message
			if i > 0 {
				prev = &log.List[i-1]
			}

			if prev == nil || !sameDay(prev.CreatedAt, m.CreatedAt) {
				d.daySeparator(m)
			}

			d.messageItem(m, prev, colors, perms)
		}
	}

	c.Pop()
}

// welcome is the transcript's first item once the whole channel is loaded.
func (d *Discordo) welcome(title string, guild bool) {
	c := d.k.Context()
	d.begin("welcome", box{axis: ui.AxisY, w: ui.Pct(1), t: 32, b: 32, l: 16, r: 16})

	d.begin("disc", box{w: ui.Px(d.px(64)), h: ui.Px(d.px(64)), radius: 32, bg: surfaceRaised, cross: ui.CrossCenter, justify: ui.JustifyCenter})
	if guild {
		d.icon("hash", 32, fog)
	} else {
		d.icon("message-circle", 32, fog)
	}
	c.Pop()

	d.begin("words", box{axis: ui.AxisY, t: 16, gap: 8, w: ui.Pct(1)})
	if guild {
		d.para("Boas-vindas a #"+title+"!", d.ui.s30b, snow)
		d.para("Este é o começo do canal #"+title+".", d.ui.s16, fog)
	} else {
		d.para(title, d.ui.s30b, snow)
		d.para("Este é o começo da sua conversa com "+title+".", d.ui.s16, fog)
	}
	c.Pop()

	c.Pop()
}

func sameDay(a, b time.Time) bool {
	a, b = a.Local(), b.Local()
	return a.YearDay() == b.YearDay() && a.Year() == b.Year()
}

// daySeparator: a line, the day, a line.
func (d *Discordo) daySeparator(m *Message) {
	c := d.k.Context()
	d.beginKey(ui.HashString(c.ParentKey(), "day "+m.ID), box{axis: ui.AxisX, w: ui.Pct(1), t: 16, b: 16, l: 16, r: 16, gap: 8, cross: ui.CrossCenter})

	c.NextSize(ui.Grow(1), ui.Px(1))
	c.NextBG(line10)
	c.Box(ui.DrawBackground, "left")

	d.text(Day(m.CreatedAt), d.ui.date, greyple)

	c.NextSize(ui.Grow(1), ui.Px(1))
	c.NextBG(line10)
	c.Box(ui.DrawBackground, "right")

	c.Pop()
}

// grouped: a message right after another of the same author within minutes (startsNewGroup).
func grouped(m, prev *Message) bool {
	return prev != nil && prev.Author.ID == m.Author.ID && m.CreatedAt.Sub(prev.CreatedAt) < 7*time.Minute && sameDay(prev.CreatedAt, m.CreatedAt)
}

var emojis = [...]string{"👍", "❤️", "😂", "😮", "😢", "🎉", "🔥", "👀"}

// messageItem is features/chat/message-item.tsx.
func (d *Discordo) messageItem(m *Message, prev *Message, colors map[string]int, perms int64) {
	k, c := d.k, d.k.Context()
	me := d.st.Me.ID
	group := grouped(m, prev)
	highlighted := m.MentionEveryone || slices.Contains(m.Mentions, me)

	key := ui.HashString(c.ParentKey(), m.ID)
	hot := k.VisualOf(key, base.HoverOn).Hot > 0.5 || d.ui.picker == m.ID

	b := box{axis: ui.AxisX, w: ui.Pct(1), l: 16, r: 16, t: 2, b: 2, gap: 16}
	if !group {
		b.t = 6 // mt-4 pt-1: the margin is a gap of its own, outside the hover colour
		c.NextSize(ui.Pct(1), ui.Px(d.px(16)))
		c.BoxKeyed(0, ui.HashString(c.ParentKey(), "gap "+m.ID), nil)
	}

	switch {
	case highlighted && hot:
		b.bg = alpha(ember, 0.15)
	case highlighted:
		b.bg = alpha(ember, 0.10)
	case hot:
		b.bg = 0x0000001a // hover:bg-black/10
	}

	flags := d.style(b) | ui.Hoverable | ui.HotAnimation
	id := c.BoxKeyed(flags, key, nil)
	c.Push(id)

	if highlighted { // border-l-2 border-ember-orange
		c.NextAnchor(ui.AnchorLeft, 0, 0)
		c.NextSize(ui.Px(d.px(2)), ui.Pct(1))
		c.NextBG(ember)
		c.Box(ui.DrawBackground, "mark")
	}

	name := m.Author.Name()
	if g := d.st.Guild(d.guild); g != nil {
		if i := slices.IndexFunc(g.Members, func(x Member) bool { return x.User.ID == m.Author.ID }); i >= 0 {
			name = g.Members[i].Name()
		}
	}

	if group {
		d.begin("time", box{axis: ui.AxisX, w: ui.Px(d.px(40)), t: 4, justify: ui.JustifyEnd})
		if hot {
			d.text(m.Time, d.ui.time, greyple)
		}
		c.Pop()
	} else {
		d.avatar(m.Author, name, 40, rexui.PresenceNone, surfaceContent)
	}

	d.begin("body", box{axis: ui.AxisY, w: ui.Grow(1), gap: 4})

	if !group {
		d.begin("head", box{axis: ui.AxisX, gap: 8, cross: ui.CrossCenter})
		color := uint32(snow)
		if rgb, ok := colors[m.Author.ID]; ok {
			color = uint32(rgb)<<8 | 0xff
		}

		d.text(name, d.ui.s14s, color)
		d.text(m.When, d.ui.time, greyple)
		c.Pop()
	}

	if len(m.Body) > 0 {
		d.messageText(m)
	}

	for i := range m.Attachments {
		d.attachment(&m.Attachments[i])
	}

	if len(m.Reactions) > 0 {
		c.BeginFlow("reactions", ui.FlowOpts{GapX: d.px(4), GapY: d.px(4)})
		for _, r := range m.Reactions {
			d.reaction(m, r, perms)
		}
		c.EndFlow()
	}

	c.Pop()

	// The hover bar: react, delete.
	own := m.Author.ID == me
	canReact, canDelete := perms&permReact != 0, own || perms&4 != 0 // manage messages
	if hot && (canReact || canDelete) {
		c.NextAnchor(ui.AnchorTopRight, d.px(16), -d.px(12))
		d.begin("actions", box{axis: ui.AxisX, bg: surfaceSidebar, radius: 6, l: 2, r: 2, t: 2, b: 2})
		c.NextBorder(line10, 1)

		if canReact {
			sig := d.iconButton("smile-plus", "Adicionar reação", 24)
			if item, ok := rexui.DropdownMenu(k.Kit, sig, d.emojiMenu); ok {
				d.toggleReaction(m, emojis[item])
			}

			d.ui.picker = ""
			if k.Popup().OpenBy(base.PopupItems, c.KeyOf(sig)) {
				d.ui.picker = m.ID
			}
		}

		if canDelete && d.clicked(d.iconButton("trash-2", "Excluir mensagem", 24)) {
			d.ui.deleting, d.ui.confirm = m, true
		}

		c.Pop()
	}

	c.Pop()
}

func (d *Discordo) emojiMenu(items *[]ui.MenuItem) {
	for i, e := range emojis {
		*items = append(*items, ui.MenuItem{ID: i, Title: e})
	}
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

// messageText is the message's text: 15 px on 24, mentions as chips.
func (d *Discordo) messageText(m *Message) {
	c := d.k.Context()
	spans := d.ui.spans[:0]

	for _, s := range m.Spans {
		style := uint16(1)
		if s.Edited {
			style = 2
		}

		spans = append(spans, ui.Span{Off: uint32(s.Off), Len: uint32(s.Len), Style: style})
	}

	d.ui.spans = spans
	c.NextFont(d.ui.s15.key)
	c.NextTextColor(fog)
	c.NextSize(ui.Pct(1), ui.TextSize(0))
	c.RichText(ui.HashString(c.ParentKey(), "text"), m.Body, spans, &d.ui.rich, ui.RichOpts{LineHeight: d.px(24)})
}

// attachment is AttachmentView: an image up to 320 pt high, or a file card.
func (d *Discordo) attachment(a *Attachment) {
	c := d.k.Context()
	path := "/attachments/" + a.ID

	if strings.HasPrefix(a.ContentType, "image/") {
		maxW, maxH := d.px(520), d.px(320)
		h, st := d.image(path, int32(maxW), int32(maxH), false)

		w, ht := d.px(320), d.px(180) // until it decodes
		if info, ok := d.imgs.Info(h); ok && st == img.Ready {
			w, ht = float32(info.W), float32(info.H)
		}

		c.NextSize(ui.Px(w), ui.Px(ht))
		d.k.Image(img.HashString(a.ID), h, ui.FitContain, d.px(8), a.Filename)

		return
	}

	d.beginKey(ui.HashString(c.ParentKey(), a.ID), box{axis: ui.AxisX, bg: surfaceRaised, radius: 8, l: 12, r: 12, t: 12, b: 12, gap: 12, cross: ui.CrossCenter})
	d.icon("file-text", 32, blurpleSoft)
	d.begin("file", box{axis: ui.AxisY})
	d.text(a.Filename, d.ui.s14m, cerulean)
	d.text(formatSize(a.Size), d.ui.s12, greyple)
	c.Pop()
	c.Pop()
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

// reaction is a reaction chip: blurple when it is mine.
func (d *Discordo) reaction(m *Message, r Reaction, perms int64) {
	c := d.k.Context()
	mine := slices.Contains(r.Users, d.st.Me.ID)

	b := box{axis: ui.AxisX, l: 6, r: 6, t: 2, b: 2, gap: 4, radius: 6, bg: surfaceRaised, cross: ui.CrossCenter}
	if mine {
		b.bg = alpha(blurple, 0.2)
		c.NextBorder(blurple, 1)
	}

	d.press(ui.HashString(c.ParentKey(), r.Emoji), b, 0, r.Emoji)
	d.text(r.Emoji, d.ui.s14, snow)

	color := uint32(fog)
	if mine {
		color = snow
	}

	d.text(itoa(len(r.Users)), d.ui.s12s, color)

	if d.pop() && (perms&permReact != 0 || mine) {
		d.toggleReaction(m, r.Emoji)
	}
}

// composerBar is features/chat/composer.tsx: the field (or why there is none) and the hint under it.
func (d *Discordo) composerBar(title string, guild bool, perms int64) {
	k, c := d.k, d.k.Context()
	d.begin("composer", box{axis: ui.AxisY, w: ui.Pct(1), l: 16, r: 16, b: 20, gap: 4})

	if perms&permSend == 0 {
		d.begin("locked", box{axis: ui.AxisX, w: ui.Pct(1), bg: surfaceRaised, radius: 8, l: 16, r: 16, t: 12, b: 12, gap: 8, cross: ui.CrossCenter})
		d.icon("lock", 16, greyple)
		d.text("Você não tem permissão para enviar mensagens neste canal.", d.ui.s14, greyple)
		c.Pop()
		c.Pop()

		return
	}

	placeholder := "Conversar com " + title
	if guild {
		placeholder = "Conversar em #" + title
	}

	if placeholder != d.ui.placeholder {
		d.ui.placeholder = placeholder
		d.composer.SetPlaceholder(placeholder)
	}

	send := rexui.ComposerOpts{Sending: d.sending, HasSendIcon: true, SendIcon: d.iconID("send-horizontal", 16)}
	if rexui.Composer(k.Kit, "Mensagem", d.composer, send).Submitted {
		d.send()
	}

	d.begin("hint", box{l: 4})
	if d.status != "" {
		d.text(d.status, d.ui.time, red)
	} else {
		d.text("Enter envia · Shift + Enter quebra a linha", d.ui.time, greyple)
	}
	c.Pop()

	c.Pop()
}

// memberList is features/members/member-list.tsx: online, then offline, each by name.
func (d *Discordo) memberList(g *Guild, H float32) {
	k, c := d.k, d.k.Context()
	colors := g.RoleColors()

	members := slices.Clone(g.Members)
	slices.SortFunc(members, func(a, b Member) int { return strings.Compare(strings.ToLower(a.Name()), strings.ToLower(b.Name())) })

	k.NextSize(ui.Grow(1), ui.Pct(1))
	v := k.BeginScroll("member list", &d.memberScroll, base.ScrollVertical)
	d.begin("groups", box{axis: ui.AxisY, w: ui.Pct(1), l: 12, r: 12, t: 24, b: 12, gap: 24})

	for _, online := range [...]bool{true, false} {
		n := 0
		for _, m := range members {
			if m.Online == online {
				n++
			}
		}

		if n == 0 {
			continue
		}

		label := "OFFLINE — "
		if online {
			label = "ONLINE — "
		}

		d.begin(label, box{axis: ui.AxisY, w: ui.Pct(1), gap: 2})
		d.begin("label", box{l: 8, r: 8, b: 6})
		d.text(label+itoa(n), d.ui.caption, greyple)
		c.Pop()

		for _, m := range members {
			if m.Online != online {
				continue
			}

			if !online {
				c.NextOpacity(0.45)
			}

			hot := d.press(ui.HashString(c.ParentKey(), m.User.ID), box{axis: ui.AxisX, w: ui.Pct(1), l: 8, r: 8, t: 6, b: 6, gap: 12, radius: 6, cross: ui.CrossCenter}, surfaceHover, m.Name())
			presence := rexui.PresenceNone
			ring := uint32(surfaceSidebar)
			if online {
				presence = rexui.PresenceOnline
			}
			if hot {
				ring = surfaceHover
			}

			d.avatar(m.User, m.Name(), 32, presence, ring)

			color := uint32(greyple)
			if hot {
				color = fog
			}
			if rgb, ok := colors[m.User.ID]; ok {
				color = uint32(rgb)<<8 | 0xff
			}

			d.text(m.Name(), d.ui.s14m, color)
			d.pop()
		}

		c.Pop()
	}

	c.Pop()
	k.CloseScrollContent(&v)
	rexui.ScrollBars(k.Kit, &v)
	k.CloseScroll(&v)
}

var friendTabs = [...]string{"Online", "Todos", "Pendentes", "Bloqueados"}

const tabAdd = 4

var friendsEmpty = [...]string{
	"Nenhum amigo online agora.",
	"Você ainda não tem amigos. Adicione alguém pelo nome de usuário.",
	"Nenhum pedido de amizade pendente.",
	"Você não bloqueou ninguém.",
}

var friendStatus = map[int]string{2: "Pedido de amizade recebido", 3: "Pedido de amizade enviado", 4: "Bloqueado"}

// friendsView is features/social/friends-view.tsx.
func (d *Discordo) friendsView() {
	k, c := d.k, d.k.Context()

	d.begin("header", box{axis: ui.AxisX, w: ui.Pct(1), h: ui.Px(d.px(headerH)), l: 16, r: 16, gap: 8, cross: ui.CrossCenter})
	d.begin("title", box{axis: ui.AxisX, gap: 8, r: 4, cross: ui.CrossCenter})
	d.icon("user-round-plus", 20, fog)
	d.text("Amigos", d.ui.s16b, snow)
	c.Pop()

	d.begin("divider", box{h: ui.Px(d.px(24))})
	d.rule(line10, true)
	c.Pop()

	for i, label := range append(friendTabs[:], "Adicionar amigo") {
		active := d.friendsTab == i
		b := box{axis: ui.AxisX, l: 10, r: 10, t: 6, b: 6, radius: 6}
		hoverBG, color := uint32(surfaceHover), uint32(greyple)

		switch {
		case i == tabAdd && active:
			b.bg, color, hoverBG = green, white, green
		case i == tabAdd:
			color, hoverBG = green, alpha(green, 0.1)
		case active:
			b.bg, color = surfaceActive, snow
		}

		hot := d.press(ui.HashString(c.ParentKey(), label), b, hoverBG, label)
		if hot && !active && i != tabAdd {
			color = fog
		}

		d.text(label, d.ui.s14s, color)
		if d.pop() {
			d.friendsTab = i
		}
	}

	c.Pop()
	d.rule(line5, false)

	if d.friendsTab == tabAdd {
		d.addFriendForm()
		return
	}

	rows := d.ui.friends[:0]
	for _, r := range d.st.Relationships {
		in := false
		switch d.friendsTab {
		case tabOnline:
			in = r.Status == 1 && r.Online
		case tabAll:
			in = r.Status == 1
		case tabPending:
			in = r.Status == 2 || r.Status == 3
		case tabBlocked:
			in = r.Status == 4
		}

		if in {
			rows = append(rows, r)
		}
	}

	slices.SortFunc(rows, func(a, b Relationship) int {
		if a.Status != b.Status {
			return a.Status - b.Status
		}

		return strings.Compare(strings.ToLower(a.User.Name()), strings.ToLower(b.User.Name()))
	})
	d.ui.friends = rows

	d.begin("count", box{w: ui.Pct(1), l: 28, r: 28, t: 20})
	d.text(strings.ToUpper(friendTabs[d.friendsTab])+" — "+itoa(len(rows)), d.ui.caption, greyple)
	c.Pop()

	k.NextSize(ui.Pct(1), ui.Grow(1))
	v := k.BeginScroll("friends", &d.friendScroll, base.ScrollVertical)
	d.begin("rows", box{axis: ui.AxisY, w: ui.Pct(1), l: 28, r: 28, t: 8, b: 16})
	c.NextMaxSize(d.px(768), 0)
	d.begin("max", box{axis: ui.AxisY, w: ui.Pct(1)})

	if len(rows) == 0 {
		d.begin("empty", box{axis: ui.AxisY, w: ui.Pct(1), t: 64, b: 64, gap: 4, cross: ui.CrossCenter})
		d.icon("ban", 36, greyple)
		d.begin("gap", box{t: 8})
		d.text("Nada por aqui", d.ui.s16b, snow)
		c.Pop()
		d.text(friendsEmpty[d.friendsTab], d.ui.s14, fog)
		c.Pop()
	}

	for i, r := range rows {
		if i > 0 {
			d.rule(line5, false)
		}

		d.friendRow(r)
	}

	c.Pop()
	c.Pop()
	k.CloseScrollContent(&v)
	rexui.ScrollBars(k.Kit, &v)
	k.CloseScroll(&v)
}

// friendRow is FriendRow: who, their status, and the actions of that status.
func (d *Discordo) friendRow(r Relationship) {
	c := d.k.Context()
	u := r.User
	d.beginKey(ui.HashString(c.ParentKey(), u.ID), box{axis: ui.AxisX, w: ui.Pct(1), t: 12, b: 12, gap: 12, cross: ui.CrossCenter})

	presence := rexui.PresenceNone
	if r.Status == 1 {
		presence = d.presence(u.ID)
	}

	d.avatar(u, u.Name(), 32, presence, surfaceContent)

	d.begin("who", box{axis: ui.AxisY, w: ui.Grow(1)})
	d.begin("names", box{axis: ui.AxisX, gap: 6, cross: ui.CrossEnd})
	d.text(u.Name(), d.ui.s14s, snow)
	d.text("@"+u.Username, d.ui.s12, greyple)
	c.Pop()

	status, ok := friendStatus[r.Status]
	if !ok {
		status = "Offline"
		if r.Online {
			status = "Online"
		}
	}

	d.text(status, d.ui.s12, greyple)
	c.Pop()

	d.begin("actions", box{axis: ui.AxisX, gap: 4})
	switch r.Status {
	case 1:
		if d.secondary("message-circle", "Enviar mensagem para "+u.Name(), 0) {
			call(d, "POST", "/dms", map[string]string{"recipient_id": u.ID}, func(d *Discordo, dm DM) { d.open("", dm.ID) })
		}

		if d.secondary("user-minus", "Desfazer amizade", 0) {
			call(d, "DELETE", "/relationships/"+u.ID, nil, func(*Discordo, struct{}) {})
		}

		if d.secondary("ban", "Bloquear "+u.Name(), 0) {
			call(d, "PUT", "/blocks/"+u.ID, nil, func(*Discordo, struct{}) {})
		}
	case 2:
		if d.secondary("check", "Aceitar pedido de "+u.Name(), green) {
			call(d, "PUT", "/relationships/"+u.ID, nil, func(*Discordo, struct{}) {})
		}

		if d.secondary("x", "Recusar pedido de "+u.Name(), 0) {
			call(d, "DELETE", "/relationships/"+u.ID, nil, func(*Discordo, struct{}) {})
		}

		if d.secondary("ban", "Bloquear "+u.Name(), 0) {
			call(d, "PUT", "/blocks/"+u.ID, nil, func(*Discordo, struct{}) {})
		}
	case 3, 4:
		label := "Cancelar"
		if r.Status == 4 {
			label = "Desbloquear"
		}

		if d.secondaryText(label) {
			call(d, "DELETE", "/relationships/"+u.ID, nil, func(*Discordo, struct{}) {})
		}
	}
	c.Pop()

	c.Pop()
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

// addFriendForm is AddFriend: a username, and the request.
func (d *Discordo) addFriendForm() {
	k, c := d.k, d.k.Context()
	d.begin("add", box{axis: ui.AxisY, w: ui.Pct(1), l: 28, r: 28, t: 20, b: 20, gap: 4})
	d.text("Adicionar amigo", d.ui.s18b, snow)
	d.text("Envie um pedido de amizade pelo nome de usuário.", d.ui.s14, fog)

	c.NextMaxSize(d.px(768), 0)
	d.begin("form", box{axis: ui.AxisX, w: ui.Pct(1), t: 12, gap: 8, cross: ui.CrossCenter})
	k.NextWidth(ui.Grow(1))
	f := k.TextField("##username", &d.ui.addFriend, "Nome de usuário")

	k.BeginDisabled(strings.TrimSpace(d.ui.addFriend.String()) == "")
	send := rexui.Button(k.Kit, "Enviar pedido", rexui.ButtonOpts{Variant: rexui.ButtonPrimary}).Clicked
	k.EndDisabled()
	c.Pop()

	if (send || f.Submitted) && strings.TrimSpace(d.ui.addFriend.String()) != "" {
		name := strings.TrimSpace(d.ui.addFriend.String())
		d.ui.addResult = ""
		call(d, "POST", "/relationships", map[string]string{"username": name}, func(d *Discordo, _ struct{}) {
			d.ui.addResult = "Pedido enviado para @" + name + "."
			d.ui.addFriend.SetString("")
		})
	}

	if d.ui.addResult != "" {
		d.text(d.ui.addResult, d.ui.s14, green)
	}

	if d.status != "" {
		d.text(d.status, d.ui.s14, red)
	}

	c.Pop()
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

// deleteDialog is the ConfirmDialog of a message's delete.
func (d *Discordo) deleteDialog() {
	k := d.k
	dlg := k.BeginDialog("delete", "Excluir mensagem", &d.ui.confirm)

	if dlg.Visible {
		k.Paragraph("Tem certeza de que quer excluir esta mensagem? Não dá para desfazer.")
		dlg.BeginActions()
		dlg.Action("Cancelar", widget.ButtonDefault)

		if dlg.Action("Excluir", widget.ButtonPrimary) && d.ui.deleting != nil {
			m := d.ui.deleting
			call(d, "DELETE", "/channels/"+m.ChannelID+"/messages/"+m.ID, nil, func(*Discordo, struct{}) {})
		}

		dlg.EndActions()
	}

	k.EndDialog(&dlg)
}

// login is features/auth/login-screen.tsx: the pitch at the left (wide windows), the card at the right.
func (d *Discordo) login(W, H float32) {
	c := d.k.Context()
	d.begin("login", box{axis: ui.AxisX, w: ui.Pct(1), h: ui.Pct(1), bg: surfaceCosmic, l: 48, r: 48, gap: 40, cross: ui.CrossCenter, justify: ui.JustifyCenter})

	if W >= d.px(1024) {
		d.begin("pitch", box{axis: ui.AxisY, w: ui.Grow(1.15)})
		c.NextMaxSize(d.px(760), 0)

		d.begin("brand", box{axis: ui.AxisX, gap: 12, cross: ui.CrossCenter})
		h, _ := d.mascot(int32(d.px(56)))
		c.NextSize(ui.Px(d.px(56)), ui.Px(d.px(56)))
		d.k.Image(3, h, ui.FitFill, d.px(16), "Discordo")
		d.begin("names", box{axis: ui.AxisY})
		d.text("Discordo", d.ui.s20b, snow)
		d.text("SEU LUGAR PARA CONVERSAR", d.ui.caption, fog)
		c.Pop()
		c.Pop()

		d.begin("headline", box{axis: ui.AxisY, w: ui.Pct(1), t: 48, b: 28})
		c.NextFont(d.ui.display.key)
		c.NextTextColor(snow)
		c.NextSize(ui.Pct(1), ui.TextSize(0))
		c.RichText(ui.HashString(c.ParentKey(), "headline"), []byte("ONDE SEU GRUPO FICA JUNTO."), nil, nil, ui.RichOpts{LineHeight: d.ui.display.line})
		c.Pop()

		d.para("Entre para conversar por texto, voz ou vídeo — com a espontaneidade de estar na mesma sala.", d.ui.s18, fog)
		c.Pop()
	}

	d.begin("card", box{axis: ui.AxisY, w: ui.Px(d.px(448)), bg: surfaceSidebar, radius: 16, l: 32, r: 32, t: 32, b: 32, gap: 8, cross: ui.CrossCenter})
	c.NextBorder(line10, 1)

	if W < d.px(1024) {
		h, _ := d.mascot(int32(d.px(64)))
		c.NextSize(ui.Px(d.px(64)), ui.Px(d.px(64)))
		d.k.Image(4, h, ui.FitFill, d.px(16), "Discordo")
	}

	d.text("Boas-vindas!", d.ui.s24b, snow)
	d.text("Entre com sua conta do Discord para continuar.", d.ui.s14, fog)

	d.begin("gap", box{t: 16})
	c.Pop()

	if d.status != "" {
		d.begin("error", box{axis: ui.AxisY, w: ui.Pct(1), bg: alpha(red, 0.1), radius: 8, l: 16, r: 16, t: 12, b: 12})
		c.NextBorder(alpha(red, 0.4), 1)
		d.para(d.status, d.ui.s14, red)
		c.Pop()
	}

	switch d.phase {
	case phaseSignedOut:
		if d.primary("Entrar com Discord") {
			d.signIn()
		}

	case phaseSigningIn:
		d.begin("waiting", box{axis: ui.AxisX, h: ui.Px(d.px(40)), gap: 8, cross: ui.CrossCenter})
		rexui.Spinner(d.k.Kit, "Entrando", rexui.SpinnerOpts{})
		d.text("Confirme no navegador e volte para cá.", d.ui.s14, fog)
		c.Pop()

	default:
		d.begin("waiting", box{axis: ui.AxisX, h: ui.Px(d.px(40)), gap: 8, cross: ui.CrossCenter})
		rexui.Spinner(d.k.Kit, "Conectando", rexui.SpinnerOpts{})
		d.text("Conectando…", d.ui.s14, fog)
		c.Pop()
	}

	d.begin("note", box{t: 12})
	d.text("Usamos só seu nome e avatar do Discord.", d.ui.s12, greyple)
	c.Pop()

	c.Pop()
	c.Pop()
}

// primary is the web's lg primary button across its parent.
func (d *Discordo) primary(label string) bool {
	c := d.k.Context()
	d.press(ui.HashString(c.ParentKey(), label), box{axis: ui.AxisX, w: ui.Pct(1), h: ui.Px(d.px(40)), radius: 8, bg: blurple, gap: 8, cross: ui.CrossCenter, justify: ui.JustifyCenter}, 0x4752c4ff, label)
	d.text(label, d.ui.s14m, white)

	return d.pop()
}
