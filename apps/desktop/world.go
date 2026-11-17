package main

// The world (docs/protocol.md, "Mundo"): a guild's map, a call where everyone stands somewhere. Walking, the rooms
// and the cinema's screen are drawn here; who hears whom is world_logic.go, the call itself voice.go.

import (
	"context"
	"math"
	"slices"
	"time"

	"discordo/desktop/voice"

	"gonativeui/rexui"
	"gonativeui/theme"
	"gonativeui/ui"
	"gonativeui/widget/base"
)

// wd is the world's state: like vc, one window and one call.
var wd struct {
	m       *WorldMap // the same for every guild, fetched once
	loading bool
	err     string
	gen     int // d.gen of the fetch: another account may be on another server

	in     bool        // I am in a world's call, and pos is where I stand
	pos    WorldPoint  // mine is local: the server does not echo my moves
	target *WorldPoint // where a click sent me
	held   [4]bool     // up, down, left, right
	sent   WorldPoint  // the last world_move
	sentAt time.Time
	cinema bool // I was in the cinema at the last frame

	last  time.Time             // the view's previous frame
	shown map[string]WorldPoint // where the others are drawn, easing toward their state
	drawn bool                  // the view was built this frame
}

var worldKeys = map[ui.KeyCode]int{
	'W': 0, ui.KeyUp: 0,
	'S': 1, ui.KeyDown: 1,
	'A': 2, ui.KeyLeft: 2,
	'D': 3, ui.KeyRight: 3,
}

// worldMap is the map, asked for the first time it is needed; nil until it arrives.
func (d *Discordo) worldMap() *WorldMap {
	if wd.gen != d.gen {
		wd.m, wd.err, wd.gen = nil, "", d.gen
	}

	if wd.m != nil || wd.loading || wd.err != "" {
		return wd.m
	}

	wd.loading = true
	client, gen := d.client, d.gen

	go func() {
		m, err := client.WorldMap(context.Background())

		d.post(func(d *Discordo) {
			wd.loading = false

			switch {
			case d.gen != gen:
			case err != nil:
				wd.err = "Não foi possível carregar o mundo: " + err.Error()
			default:
				wd.m = m
			}
		})
	}()

	return nil
}

func worldZone(id string) *WorldZone {
	if wd.m == nil || id == "" {
		return nil
	}

	for i := range wd.m.Zones {
		if wd.m.Zones[i].ID == id {
			return &wd.m.Zones[i]
		}
	}

	return nil
}

// worldStatus is what the guild sees of someone in a room ("Jogando Valorant"); "" in the plaza or without the map.
func (d *Discordo) worldStatus(zone string) string {
	if zone == "" || d.worldMap() == nil {
		return ""
	}

	if z := worldZone(zone); z != nil {
		return z.Status
	}

	return ""
}

// worldCall is the world whose call I am in, or nil.
func (d *Discordo) worldCall() *Channel {
	if vc.channel == "" {
		return nil
	}

	for i := range d.st.Guilds {
		if c := d.st.Guilds[i].Channel(vc.channel); c != nil && c.Type == ChannelWorld {
			return c
		}
	}

	return nil
}

// worldGain is how well I hear someone from where I stand: 1 outside a world.
func (d *Discordo) worldGain(userID string) float32 {
	me, ok := d.st.Voice[d.st.Me.ID]
	other, okOther := d.st.Voice[userID]
	if !ok || !okOther || me.ChannelID != vc.channel || other.ChannelID != vc.channel || d.worldCall() == nil {
		return 1
	}

	return gain(me, other)
}

// worldScreening is whose screen the cinema shows me: the first sharer inside, while I am inside too.
func (d *Discordo) worldScreening() string {
	me := d.st.Voice[d.st.Me.ID]
	if z := worldZone(me.Zone); z == nil || z.Kind != "cinema" || me.ChannelID != vc.channel {
		return ""
	}

	for _, v := range d.st.Callers(vc.channel) {
		published := slices.Contains(v.Tracks, voice.TrackScreen)
		if v.UserID == me.UserID {
			published = vc.s != nil && vc.s.Sharing()
		}

		if published && v.Zone == me.Zone {
			return v.UserID
		}
	}

	return ""
}

// worldSync runs every frame, whatever is on screen: my state follows where I stand, the others hear my moves,
// everyone's volume follows the distances, and the cinema's screen is watched only from inside.
func (d *Discordo) worldSync() {
	c := d.k.Context()
	drawn := wd.drawn
	wd.drawn = false

	me, ok := d.st.Voice[d.st.Me.ID]
	if !ok || me.ChannelID != vc.channel || d.worldCall() == nil {
		wd.in, wd.cinema, wd.target, wd.held = false, false, nil, [4]bool{}
		return
	}

	if !wd.in {
		wd.in, wd.pos, wd.sent = true, WorldPoint{me.X, me.Y}, WorldPoint{me.X, me.Y}
	}

	if !drawn {
		wd.target, wd.held = nil, [4]bool{} // nobody walks a map that is not on screen
	}

	cinema := false
	me.X, me.Y = wd.pos.X, wd.pos.Y
	if m := d.worldMap(); m != nil {
		me.Zone = ""
		if z := zoneAt(m, me.X, me.Y); z != nil {
			me.Zone, cinema = z.ID, z.Kind == "cinema"
		}
	}

	d.st.Voice[me.UserID] = me

	if vc.s == nil {
		return
	}

	// 10 a second while walking; the last one, once stopped, is where I stay.
	if now := time.Now(); wd.pos != wd.sent {
		if now.Sub(wd.sentAt) >= 100*time.Millisecond {
			vc.s.Move(wd.pos.X, wd.pos.Y)
			wd.sent, wd.sentAt = wd.pos, now
		} else {
			c.RequestFrame()
		}
	}

	screening := d.worldScreening()
	for _, v := range d.st.Callers(vc.channel) {
		if v.UserID == me.UserID {
			continue
		}

		d.voiceApplyVolume(v.UserID)

		if on := v.UserID == screening; on != vc.scr.watching[v.UserID] {
			voiceWatch(v.UserID, on)
		}
	}

	// Walking out of the cinema ends my share (or its preview).
	if wd.cinema && !cinema {
		vc.scr.dialog = false
		voiceStopShare()
	}

	wd.cinema = cinema
}

// worldView is the world's main area: the map, where to go, and joining or the call's controls.
func (d *Discordo) worldView(g *Guild, ch *Channel) {
	c := d.k.Context()
	m := d.worldMap()
	callers := d.st.Callers(ch.ID)
	mine := wd.in && vc.channel == ch.ID // I walk this map
	wd.drawn = true

	d.begin("world", box{axis: ui.AxisY, w: ui.Pct(1), h: ui.Pct(1), bg: surfaceRail, l: 16, r: 16, t: 16, b: 16, gap: 12})

	d.begin("header", box{axis: ui.AxisX, w: ui.Pct(1), gap: 6, cross: ui.CrossCenter, clip: true})
	d.begin("title", box{axis: ui.AxisY, w: ui.Grow(1)})
	d.text(ch.Name, d.ui.s16b, snow)
	d.text(plural(len(callers), "pessoa", "pessoas"), d.ui.s12, greyple)
	c.Pop()

	if mine && m != nil {
		d.text("Ir para", d.ui.s12, greyple)

		to := (*WorldPoint)(nil)
		if d.secondaryText("Praça") {
			to = &m.Spawn
		}

		for i := range m.Zones {
			if d.secondaryText(m.Zones[i].Name) {
				to = &m.Zones[i].Spot
			}
		}

		if to != nil {
			wd.pos, wd.target = *to, nil
		}
	}
	c.Pop()

	switch {
	case m != nil:
		d.worldStage(g, m, callers, mine)

	case wd.err != "":
		d.begin("failed", box{axis: ui.AxisY, w: ui.Pct(1), h: ui.Grow(1), gap: 12, cross: ui.CrossCenter, justify: ui.JustifyCenter})
		d.text(wd.err, d.ui.s14, red)
		if d.secondaryText("Tentar de novo") {
			wd.err = ""
		}
		c.Pop()

	default:
		d.begin("loading", box{axis: ui.AxisY, w: ui.Pct(1), h: ui.Grow(1), cross: ui.CrossCenter, justify: ui.JustifyCenter})
		rexui.Spinner(d.k.Kit, "Carregando o mundo", rexui.SpinnerOpts{})
		c.Pop()
	}

	d.voiceFooter(ch, "Entrar no mundo", wd.cinema)
	c.Pop()
}

// worldCam is where the view starts on one axis, in pixels: centred on focus and held inside the map; a map
// smaller than the view is centred.
func worldCam(focus, view, size float32) float32 {
	if size <= view {
		return (size - view) / 2
	}

	return max(0, min(size-view, focus-view/2))
}

// worldStage draws the map through a camera, and walks me when mine: keys, or a click to walk to.
func (d *Discordo) worldStage(g *Guild, m *WorldMap, callers []VoiceState, mine bool) {
	k, c := d.k, d.k.Context()

	// The view's size is the last layout's: nothing to draw into before the first.
	key := ui.HashString(c.ParentKey(), "map")
	r := c.RectOf(key)
	vw, vh := r[2]-r[0], r[3]-r[1]

	c.NextSemName("Mapa do mundo")
	id := c.BoxKeyed(d.style(box{w: ui.Pct(1), h: ui.Grow(1), radius: 12, bg: canvas, clip: true})|k.InteractFlags(base.HoverOn), key, nil)
	c.Push(id)

	if vw <= 0 || vh <= 0 {
		c.RequestFrame()
		c.Pop()

		return
	}

	// Real time, bounded so that a late frame never jumps a wall.
	now := time.Now()
	dt := float32(min(now.Sub(wd.last).Seconds(), 0.05))
	wd.last = now

	// A map unit is a point while I walk; a visitor sees the whole map.
	z, focus := d.ui.scale, m.Spawn
	if mine {
		focus = wd.pos
	} else {
		z = min(z, vw/m.Width, vh/m.Height)
	}

	camX, camY := worldCam(focus.X*z, vw, m.Width*z), worldCam(focus.Y*z, vh, m.Height*z)

	if mine {
		if s := c.Signal(id); s.Clicked {
			wd.target = &WorldPoint{(s.Mouse[0] + camX) / z, (s.Mouse[1] + camY) / z}
		}

		d.worldWalk(m, dt)
		camX, camY = worldCam(wd.pos.X*z, vw, m.Width*z), worldCam(wd.pos.Y*z, vh, m.Height*z)
	}

	// at places the next box on a rectangle of the map; false when none of it is in view.
	at := func(x, y, w, h float32) bool {
		x0, y0 := x*z-camX, y*z-camY
		if x0 >= vw || y0 >= vh || x0+w*z <= 0 || y0+h*z <= 0 {
			return false
		}

		theme.At(c, theme.Snap(x0), theme.Snap(y0), theme.Snap(w*z), theme.Snap(h*z))

		return true
	}

	if at(0, 0, m.Width, m.Height) {
		d.begin("floor", box{bg: surfaceContent, floating: true})
		c.Pop()
	}

	// A faint grid, so that walking the open plaza shows.
	for x := float32(200); x < m.Width; x += 200 {
		if at(x, 0, 1/z, m.Height) {
			d.begin("grid x "+itoa(int(x)), box{bg: line5, floating: true})
			c.Pop()
		}
	}

	for y := float32(200); y < m.Height; y += 200 {
		if at(0, y, m.Width, 1/z) {
			d.begin("grid y "+itoa(int(y)), box{bg: line5, floating: true})
			c.Pop()
		}
	}

	for i := range m.Zones {
		zn := &m.Zones[i]
		if at(zn.X, zn.Y, zn.W, zn.H) {
			d.begin("zone "+zn.ID, box{axis: ui.AxisY, bg: alpha(zn.Color<<8, 0.16), floating: true, l: 28, t: 24, clip: true})
			d.text(zn.Name, d.ui.s16b, snow)
			c.Pop()
		}

		if s := zn.Screen; s != nil && at(s.X, s.Y, s.W, s.H) {
			outline(c, line10, d.px(2))
			d.begin("screen "+zn.ID, box{axis: ui.AxisY, bg: 0x000000ff, radius: 12, floating: true, clip: true, gap: 8, cross: ui.CrossCenter, justify: ui.JustifyCenter})

			if who := d.worldScreening(); mine && who != "" {
				own := who == d.st.Me.ID
				d.voiceScreenTile(g, voiceStageItem{st: d.st.Voice[who], screen: true, own: own, live: true, watched: !own, inCall: true}, ui.Pct(1), ui.Pct(1), false, true)
			} else {
				d.icon("film", 32, greyple)
				d.text("Telão", d.ui.s14m, greyple)
			}

			c.Pop()
		}
	}

	for i, w := range m.Walls {
		if at(w.X, w.Y, w.W, w.H) {
			color := greyple
			if zn := zoneAt(m, w.X, w.Y); zn != nil {
				color = zn.Color<<8 | 0xff
			}

			d.begin("wall "+itoa(i), box{bg: color, floating: true})
			c.Pop()
		}
	}

	if wd.shown == nil {
		wd.shown = map[string]WorldPoint{}
	}

	size := max(24, 2*worldRadius*z/d.ui.scale) // the avatar, in points: still a face on a visitor's small map
	pad := d.px(4)                              // the ring's room around it
	colW := d.px(160)                           // the avatar and the name under it

	for _, v := range callers {
		p := WorldPoint{v.X, v.Y}
		own := v.UserID == d.st.Me.ID

		switch {
		case own && mine:
			p = wd.pos
		default:
			// The others ease toward where they are; a teleport ("Ir para") does not slide across the map.
			from, seen := wd.shown[v.UserID]
			dx, dy := p.X-from.X, p.Y-from.Y
			if dist := math.Hypot(float64(dx), float64(dy)); seen && dist > 0.5 && dist < worldFar {
				rate := float32(1 - math.Exp(float64(-12*dt)))
				p = WorldPoint{from.X + dx*rate, from.Y + dy*rate}
				c.RequestFrame()
			}

			wd.shown[v.UserID] = p
		}

		x0, y0 := p.X*z-camX-colW/2, p.Y*z-camY-d.px(size)/2-pad
		if x0 >= vw || y0 >= vh || x0+colW <= 0 || y0+d.px(size+40) <= 0 {
			continue
		}

		name, u := d.voiceName(g, v.UserID), User{ID: v.UserID}
		if own {
			u = d.st.Me
		} else if i := slices.IndexFunc(g.Members, func(m Member) bool { return m.User.ID == v.UserID }); i >= 0 {
			u = g.Members[i].User
		}

		c.NextPos(theme.Snap(x0), theme.Snap(y0))
		d.begin("who "+v.UserID, box{axis: ui.AxisY, w: ui.Px(colW), gap: 2, cross: ui.CrossCenter, floating: true})

		// Speaking rings green; mine is marked, so I find myself in a crowd.
		ring := uint32(0)
		switch {
		case mine && d.voiceSpeaking(v.UserID):
			ring = mediaSpeaking
		case own:
			ring = blurpleSoft
		}

		outline(c, ring, d.px(3))
		d.begin("ring", box{radius: size/2 + 4, l: 4, r: 4, t: 4, b: 4})
		d.avatar(u, name, size, rexui.PresenceNone, surfaceContent)
		c.Pop()

		d.begin("name", box{axis: ui.AxisX, l: 6, r: 6, t: 2, b: 2, gap: 4, radius: 6, bg: 0x000000a6, cross: ui.CrossCenter})
		if v.Muted {
			d.icon("mic-off", 12, mediaMuted)
		}

		d.text(name, d.ui.s12s, white)
		c.Pop()
		c.Pop()
	}

	c.Pop()
}

// worldWalk moves me for dt seconds: the held keys, else toward the clicked point until there or stopped by a wall.
func (d *Discordo) worldWalk(m *WorldMap, dt float32) {
	c := d.k.Context()

	// Keys are held across frames, and a text field or a dialog keeps what is typed into it.
	for i := range c.KeyCount() {
		e, claimed := c.KeyAt(i)
		dir, ok := worldKeys[e.Code]

		switch {
		case !ok:
		case !e.Down:
			wd.held[dir] = false
		case !claimed && e.Mods&^ui.ModShift == 0 && !c.TextFieldFocused() && !c.ModalOpen():
			wd.held[dir] = true
			c.ClaimKey(i)
		}
	}

	if !c.Input().Focused {
		wd.held = [4]bool{} // the key's release went to another window
	}

	axis := func(neg, pos bool) float32 {
		switch {
		case pos && !neg:
			return 1
		case neg && !pos:
			return -1
		}

		return 0
	}

	dir := WorldPoint{axis(wd.held[2], wd.held[3]), axis(wd.held[0], wd.held[1])}
	bounds := WorldPoint{m.Width, m.Height}

	switch {
	case dir != WorldPoint{}:
		wd.target = nil
		wd.pos = step(wd.pos, dir, dt, m.Walls, bounds)
		c.RequestFrame()

	case wd.target != nil:
		to := WorldPoint{wd.target.X - wd.pos.X, wd.target.Y - wd.pos.Y}
		dist := float32(math.Hypot(float64(to.X), float64(to.Y)))
		next := step(wd.pos, to, min(dt, dist/worldSpeed), m.Walls, bounds)

		if next == wd.pos || dist <= worldSpeed*dt {
			wd.target = nil // there, or a wall is in the way
		}

		wd.pos = next
		c.RequestFrame()
	}
}
