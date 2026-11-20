package main

// The world (docs/protocol.md, "Mundo"): a guild's map, a call where everyone stands somewhere. The server paints
// the map and the characters; walking, the names and the cinema's screen are drawn here; who hears whom is
// world_logic.go, the call itself voice.go.

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"net/url"
	"slices"
	"time"

	"discordo/desktop/voice"

	"gonativeui/img"
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
	moved  time.Time   // the last time walking there got me anywhere
	held   [4]bool     // up, down, left, right
	sent   WorldPoint  // the last world_move
	sentAt time.Time
	cinema bool // I was in the cinema at the last frame

	skins       []WorldSkin // nil until asked for, with the dialog
	skinOpen    bool        // the skin dialog
	expanded    bool        // the cinema's screen, enlarged in a dialog
	skinPending bool        // the saved skin still has to be put on in this call: the server forgets it

	last    time.Time             // the view's previous frame
	shown   map[string]worldShown // how everyone was drawn in the last frame
	sprites map[string][][]byte   // sheet → its 12 frames, cut once (nil: not a sheet)
	drawn   bool                  // the view was built this frame
}

// worldShown is where a character was drawn (the others ease toward their state) and the row of the sheet it faces.
type worldShown struct {
	pos  WorldPoint
	face int
}

// worldSprite is a frame (row × 3 + column) of someone's sheet; not Ready until the sheet arrived.
func (d *Discordo) worldSprite(v VoiceState, frame int) (img.Handle, img.State) {
	seed := v.Skin
	if seed == "" {
		seed = v.UserID
	}

	ref := "/world/sprites/" + url.PathEscape(seed)
	p := d.fetched(ref)
	if p == nil {
		return 0, img.Failed
	}

	frames, cut := wd.sprites[ref]
	if !cut {
		if wd.sprites == nil {
			wd.sprites = map[string][][]byte{}
		}

		frames = cutSprite(p.data)
		wd.sprites[ref] = frames
	}

	if frames == nil {
		return 0, img.Failed
	}

	return d.request(p.hash+uint64(frame)+1, frames[frame], 0, 0, false)
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
		wd.in, wd.cinema, wd.target, wd.held, wd.skinOpen = false, false, nil, [4]bool{}, false
		return
	}

	if !wd.in {
		wd.in, wd.pos, wd.sent = true, WorldPoint{me.X, me.Y}, WorldPoint{me.X, me.Y}
		wd.skinPending = savedWorldSkin() != ""
	}

	if wd.skinPending && vc.s != nil {
		wd.skinPending = false
		vc.s.WearSkin(savedWorldSkin())
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

	if mine && d.secondaryText("Skin") {
		wd.skinOpen = true
	}

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

	if mine && vc.s != nil {
		d.begin("reactions", box{axis: ui.AxisX, w: ui.Pct(1), gap: 4, justify: ui.JustifyCenter})
		for _, e := range emojis { // the chat's, which the server takes
			if d.secondaryText(e) {
				d.st.React(d.st.Me.ID, e, time.Now())
				vc.s.React(e)
			}
		}
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

// The name over a character, in points.
const (
	pillH         = 20
	pillBG uint32 = 0x111214d9
)

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
			wd.target, wd.moved = &WorldPoint{(s.Mouse[0] + camX) / z, (s.Mouse[1] + camY) / z}, now
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

	// The whole map is one picture, at its own size: decoded smaller it would lose its pixels.
	if at(0, 0, m.Width, m.Height) {
		pic, _ := d.image("/world/map.png", 0, 0, false)
		c.NextFlags(base.Floating)
		k.Image(1, pic, ui.FitFill, 0, "")
	}

	for i := range m.Zones {
		zn := &m.Zones[i]

		// A film on: the cinema's lights go down around the screen.
		showing, audience := "", 0
		if zn.Screen != nil {
			for _, v := range callers {
				if v.Zone == zn.ID && slices.Contains(v.Tracks, voice.TrackScreen) && showing == "" {
					showing = v.UserID
				}
			}

			for _, v := range callers {
				if v.Zone == zn.ID && v.UserID != showing {
					audience++
				}
			}
		}

		if showing != "" && at(zn.X, zn.Y, zn.W, zn.H) {
			d.begin("dim "+zn.ID, box{floating: true, bg: 0x08061466})
			c.Pop()
		}

		if s := zn.Screen; s != nil && at(s.X, s.Y, s.W, s.H) {
			d.begin("screen "+zn.ID, box{axis: ui.AxisY, floating: true, clip: true, cross: ui.CrossCenter, justify: ui.JustifyCenter, gap: 4})

			who := d.worldScreening()
			inside := mine && d.st.Voice[d.st.Me.ID].Zone == zn.ID
			note := func(title, line string) {
				d.text(title, d.ui.s14m, alpha(greyple, 0.8))
				d.text(line, d.ui.s12, alpha(greyple, 0.6))
			}

			switch {
			case mine && who == d.st.Me.ID:
				// Not my own screen on the screen: sharing all of it, that would be a mirror in a mirror.
				note("Você está transmitindo", watching(audience))

			case mine && who != "" && wd.expanded:
				note("Assistindo em tela cheia", "Feche a tela cheia para voltar a ver aqui.")

			case mine && who != "":
				d.voiceScreenTile(g, voiceStageItem{st: d.st.Voice[who], screen: true, live: true, watched: true, inCall: true}, ui.Pct(1), ui.Pct(1), false, true)
				c.NextAnchor(ui.AnchorTopRight, d.px(8), d.px(8))
				d.begin("expand", box{})
				if d.voiceStageButton("maximize-2", "Ampliar o telão") {
					wd.expanded = true
				}
				c.Pop()

			case showing != "" && inside:
				note("Transmissão em andamento", "Carregando…")

			case showing != "":
				note("Transmissão em andamento", "Entre no cinema para assistir.")

			case inside:
				note("Ninguém transmitindo", "Use o botão de compartilhar tela para transmitir aqui.")

			default:
				note("Ninguém transmitindo", "Entre no cinema para compartilhar sua tela.")
			}

			c.Pop()
		}
	}

	if wd.shown == nil {
		wd.shown = map[string]worldShown{}
	}

	type actor struct {
		v      VoiceState
		x, y   float32 // the feet, in the view's pixels
		sprite img.Handle
	}

	actors := make([]actor, 0, len(callers))
	cz := max(z, d.ui.scale/2) // a visitor's small map keeps characters one can tell apart
	colW := d.px(200)          // the room a name is centred in

	for _, v := range callers {
		p := WorldPoint{v.X, v.Y}
		was, seen := wd.shown[v.UserID]

		if v.UserID == d.st.Me.ID && mine {
			p = wd.pos
		} else if dist := math.Hypot(float64(p.X-was.pos.X), float64(p.Y-was.pos.Y)); seen && dist > 0.5 && dist < worldFar {
			// The others ease toward where they are; a teleport ("Ir para") does not slide across the map.
			rate := float32(1 - math.Exp(float64(-12*dt)))
			p = WorldPoint{was.pos.X + (p.X-was.pos.X)*rate, was.pos.Y + (p.Y-was.pos.Y)*rate}
			c.RequestFrame()
		}

		// Walking is moving faster than an eased stop's last crawl.
		dx, dy := p.X-was.pos.X, p.Y-was.pos.Y
		dist := float32(math.Hypot(float64(dx), float64(dy)))
		walking := seen && dist > max(20*dt, 0.01) && dist < worldFar

		frame, face := spriteFrame(was.face, dx, dy, walking, now.UnixMilli())
		wd.shown[v.UserID] = worldShown{p, face}

		if walking {
			c.RequestFrame() // the frame after the last step is the one standing
		}

		a := actor{v: v, x: p.X*z - camX, y: p.Y*z - camY}
		if a.x+colW/2 <= 0 || a.x-colW/2 >= vw || a.y+spriteFeet*cz <= 0 || a.y-spriteH*cz-d.px(pillH) >= vh {
			continue
		}

		// A step not decoded yet shows the standing frame.
		st := img.Failed
		if a.sprite, st = d.worldSprite(v, frame); st != img.Ready {
			a.sprite, _ = d.worldSprite(v, face*3)
		}

		actors = append(actors, a)
	}

	// Whoever stands lower is in front.
	slices.SortStableFunc(actors, func(a, b actor) int { return cmp.Compare(a.y, b.y) })

	for _, a := range actors {
		if k.ImageState(a.sprite) != img.Ready {
			continue
		}

		theme.At(c, theme.Snap(a.x-spriteW/2*cz), theme.Snap(a.y-(spriteH-spriteFeet)*cz), theme.Snap(spriteW*cz), theme.Snap(spriteH*cz))
		c.NextFlags(base.Floating)
		k.Image(img.HashString(a.v.UserID), a.sprite, ui.FitFill, 0, "")
	}

	// The names go over every character, as Gather's: a dark pill with how they are in the call.
	for _, a := range actors {
		c.NextPos(theme.Snap(a.x-colW/2), theme.Snap(a.y-(spriteH-spriteFeet)*cz)-d.px(pillH))
		d.begin("who "+a.v.UserID, box{axis: ui.AxisX, w: ui.Px(colW), justify: ui.JustifyCenter, floating: true})
		d.begin("name", box{axis: ui.AxisX, h: ui.Px(d.px(pillH)), l: 7, r: 8, gap: 5, radius: pillH / 2, bg: pillBG, cross: ui.CrossCenter})

		switch {
		case a.v.Deafened:
			d.icon("headphone-off", 12, mediaMuted)

		case a.v.Muted:
			d.icon("mic-off", 12, mediaMuted)

		case mine && d.voiceSpeaking(a.v.UserID):
			outline(c, white, d.px(2))
			d.begin("dot", box{w: ui.Px(d.px(10)), h: ui.Px(d.px(10)), radius: 5, bg: mediaSpeaking})
			c.Pop()

		default:
			d.begin("dot", box{w: ui.Px(d.px(8)), h: ui.Px(d.px(8)), radius: 4, bg: mediaSpeaking})
			c.Pop()
		}

		d.text(d.voiceName(g, a.v.UserID), d.ui.s12s, white)
		c.Pop()
		c.Pop()
	}

	// Reactions rise from over the name and fade.
	for i, r := range d.st.Reactions {
		t := float32(now.Sub(r.At)) / float32(reactionFor)
		if t < 0 || t >= 1 {
			continue
		}

		c.RequestFrame()
		for _, a := range actors {
			if a.v.UserID != r.UserID {
				continue
			}

			y := a.y - (spriteH-spriteFeet)*cz - d.px(pillH) - d.px(30) - t*d.px(40)
			c.NextPos(theme.Snap(a.x-colW/2), theme.Snap(y))
			d.begin(fmt.Sprint("react ", i), box{axis: ui.AxisX, w: ui.Px(colW), justify: ui.JustifyCenter, floating: true})
			d.text(r.Emoji, d.ui.s16b, alpha(white, 1-t*t))
			c.Pop()
		}
	}

	c.Pop()
}

// worldWalk moves me for dt seconds: the held keys, else toward the clicked point until there or stuck.
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

		now := time.Now()
		if next != wd.pos {
			wd.moved = now
		}

		// Furniture and walls are slid along; getting nowhere for a while is giving up.
		if dist <= worldSpeed*dt || now.Sub(wd.moved) > 300*time.Millisecond {
			wd.target = nil
		}

		wd.pos = next
		c.RequestFrame()
	}
}

func savedWorldSkin() string {
	if themeStore == nil {
		return ""
	}

	return themeStore.Get().WorldSkin
}

// worldSkinDialog picks the character I wear, saved for every world: my own, or one of the server's skins.
func (d *Discordo) worldSkinDialog() {
	if wd.skinOpen && wd.skins == nil {
		wd.skins = []WorldSkin{} // asked once
		call(d, "GET", "/world/skins", nil, func(_ *Discordo, s []WorldSkin) { wd.skins = s })
	}

	k := d.k
	dlg := k.BeginDialog("world-skin", "Skin", &wd.skinOpen)

	if dlg.Visible {
		c := k.Context()
		pick := (*string)(nil)
		none := ""

		if d.secondaryText("Meu personagem") {
			pick = &none
		}

		for i := 0; i < len(wd.skins); i += 4 {
			d.begin(fmt.Sprint("row", i), box{axis: ui.AxisX, gap: 6})
			for j := i; j < min(i+4, len(wd.skins)); j++ {
				if d.secondaryText(wd.skins[j].Name) {
					pick = &wd.skins[j].ID
				}
			}
			c.Pop()
		}

		if pick != nil {
			wd.skinOpen = false
			if themeStore != nil {
				themeStore.Update(func(v *themePrefs) { v.WorldSkin = *pick })
			}

			if vc.s != nil {
				vc.s.WearSkin(*pick)
			}
		}
	}

	k.EndDialog(&dlg)
}

// worldCinemaDialog is the enlarged cinema screen ("Ampliar o telão"); it closes when the film ends.
func (d *Discordo) worldCinemaDialog() {
	who := d.worldScreening()
	if who == "" || who == d.st.Me.ID {
		wd.expanded = false
	}

	k := d.k
	dlg := k.BeginDialog("world-cinema", "Telão do cinema", &wd.expanded)
	if dlg.Visible && who != "" {
		ww, _ := k.Context().Size()
		w := min(d.px(960), float32(ww)*0.8)
		d.voiceScreenTile(d.st.Guild(d.guild), voiceStageItem{st: d.st.Voice[who], screen: true, live: true, watched: true, inCall: true},
			ui.Px(w), ui.Px(w*9/16), false, true)
	}

	k.EndDialog(&dlg)
}

func watching(n int) string {
	if n == 0 {
		return "ninguém assistindo"
	}

	return itoa(n) + " assistindo"
}
