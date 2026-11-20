package main

// Calls (features/voice): joining a voice channel, the stage with everyone's tiles, the controls, the dock.
// The media side (devices, Opus, WebRTC with the server's SFU) is package voice; this file is its window.

import (
	"errors"
	"os"
	"slices"
	"strings"
	"time"

	"discordo/desktop/voice"

	"gonativeui/app/perm"
	"gonativeui/platform"
	"gonativeui/rexui"
	"gonativeui/theme"
	"gonativeui/ui"
	"gonativeui/widget/base"
)

// vc is the call's state: the shared Discordo struct is frozen, and there is one window.
type voiceDevices struct {
	loaded, loading, drawn bool
	inputs, outputs        []voice.Device
}

var vc struct {
	s        *voice.Session
	channel  string // the call's channel; "" when in none
	gen      int    // d.gen at the join: signing out hangs up
	starting bool   // asking for the microphone, before the session exists

	muted, deafened bool // my choices, kept across calls as the web keeps them

	ended, endedIn string // what ended the last call by itself, and where (shown on that stage)

	settings bool // the devices dialog
	scr      voiceScreens
	scroll   base.ScrollState
	devs     voiceDevices
	devErr   string

	// shortcut: devices and volumes last until the app quits; save them in the config dir if asked.
	input, output string
	volumes       map[string]float32 // by user, 0 to 2
}

// The web's colours for the call (design/tokens.css: media-*).
var (
	mediaConnecting uint32 = 0xf0b232ff
	mediaSpeaking          = green
	mediaMuted             = red
)

// voiceJoin enters a call (a voice channel, or a DM with all permissions). perms says whether I may speak.
func (d *Discordo) voiceJoin(channelID string, perms int64) {
	if vc.channel == channelID && vc.starting {
		return
	}

	if vc.channel == channelID && vc.s != nil {
		if st, _ := vc.s.Status(); st != voice.Failed {
			return
		}
	}

	d.voiceLeave()
	vc.channel, vc.gen, vc.starting, vc.ended = channelID, d.gen, true, ""

	opt := voice.Options{Server: d.client.Server, Token: d.client.Token, ChannelID: channelID, Input: vc.input, Output: vc.output,
		Muted: vc.muted, Deafened: vc.deafened, OnChange: platform.Wake}
	speak, gen := has(perms, permSpeak), d.gen

	// The system may ask the user for the microphone: never on the UI thread.
	go func() {
		reason := ""
		if speak {
			reason = microphoneRefusal()
		}

		opt.ListenOnly, opt.ListenOnlyReason = !speak || reason != "", reason

		d.post(func(d *Discordo) {
			if d.gen != gen || vc.channel != channelID || !vc.starting {
				return // left (or signed out) while asking
			}

			vc.starting = false
			vc.s = voice.Join(opt)
			for u := range vc.volumes {
				d.voiceApplyVolume(u)
			}
		})
	}()
}

// voiceLeave hangs up.
func (d *Discordo) voiceLeave() {
	if vc.s != nil {
		vc.s.Leave()
	}

	vc.s, vc.channel, vc.starting = nil, "", false
	voiceScreenReset()
}

// voiceApplyVolume plays someone as loud as I chose, times how well I hear them from where I stand in a world.
func (d *Discordo) voiceApplyVolume(userID string) {
	v, ok := vc.volumes[userID]
	if !ok {
		v = 1
	}

	vc.s.SetVolume(userID, v*d.worldGain(userID))
}

// voiceStatus is the call's state: connecting while the microphone is asked for.
func voiceStatus() (voice.Status, string) {
	if vc.s == nil {
		return voice.Connecting, ""
	}

	return vc.s.Status()
}

// voiceSpeaking says whether someone in my call speaks now (for rings in other lists).
func (d *Discordo) voiceSpeaking(userID string) bool {
	return vc.s != nil && vc.s.Speaking(userID)
}

func voiceListenOnly() bool { return vc.s != nil && vc.s.ListenOnly() }

func voiceSetMuted(muted bool) {
	vc.muted = muted
	if !muted {
		vc.deafened = false // unmuting takes the headphones back too, as the web
	}

	if vc.s != nil {
		vc.s.SetMuted(vc.muted, vc.deafened)
	}
}

func voiceSetDeafened(deafened bool) {
	vc.deafened = deafened
	if vc.s != nil {
		vc.s.SetMuted(vc.muted, vc.deafened)
	}
}

// microphoneRefusal asks for the microphone where the system protects it (macOS) and says why it cannot be used
// ("" when it can). It blocks until the user answers.
func microphoneRefusal() string {
	st, err := perm.Status(perm.Microphone)
	if err != nil {
		return "" // Windows and Linux: nothing to ask here
	}

	if st == perm.NotDetermined {
		_, err := perm.Request(perm.Microphone)
		if errors.Is(err, perm.ErrNoUsageDescription) {
			exe, _ := os.Executable()
			if !strings.Contains(exe, ".app/Contents/MacOS/") {
				return "" // a development run (go run): the system asks on behalf of the terminal
			}

			return "Este app não tem a descrição de uso do microfone (NSMicrophoneUsageDescription). Você está só ouvindo."
		}

		if err != nil {
			return "Não foi possível pedir o microfone: " + err.Error() + ". Você está só ouvindo."
		}

		for range 600 { // up to 2 minutes for the user to answer
			time.Sleep(200 * time.Millisecond)
			if st, _ = perm.Status(perm.Microphone); st != perm.NotDetermined {
				break
			}
		}
	}

	if st != perm.Authorized {
		return "O acesso ao microfone foi negado. Libere o Discordo em Ajustes do Sistema › Privacidade › Microfone. Você está só ouvindo."
	}

	return ""
}

// voiceTitle is "canal / servidor" for a voice channel, the DM's name for a DM (voice-dock.tsx's useCallTitle).
func (d *Discordo) voiceTitle(channelID string) string {
	for i := range d.st.Guilds {
		g := &d.st.Guilds[i]
		if c := g.Channel(channelID); c != nil {
			return c.Name + " / " + g.Name
		}
	}

	if dm := d.st.DM(channelID); dm != nil {
		if name := d.st.DMName(*dm); name != "" {
			return name
		}
	}

	return "Chamada"
}

// voiceControl is voice-controls.tsx's Control: an outline toggle, red when danger and pressed.
func (d *Discordo) voiceControl(iconName, label string, size float32, pressed, disabled, danger bool) bool {
	c := d.k.Context()
	bg, hover, fg := uint32(0), uint32(surfaceHover), uint32(snow)

	switch {
	case pressed && danger:
		bg, hover, fg = mediaMuted, 0xda373cff, white
	case pressed:
		bg, hover = surfaceActive, surfaceActive
	}

	if disabled {
		hover, fg = 0, alpha(fg, 0.5)
	}

	outline(c, line10, 1)
	d.press(ui.HashString(c.ParentKey(), label), box{w: ui.Px(d.px(size)), h: ui.Px(d.px(size)), radius: 8, bg: bg, cross: ui.CrossCenter, justify: ui.JustifyCenter}, hover, label)
	d.icon(iconName, 16, fg)
	id := c.Parent()
	clicked := d.pop()
	rexui.Tooltip(d.k.Kit, id, label)

	return clicked && !disabled
}

// voiceHangUp is the destructive "Sair da chamada" button.
func (d *Discordo) voiceHangUp(size float32) {
	c := d.k.Context()
	label := "Sair da chamada"
	d.press(ui.HashString(c.ParentKey(), label), box{w: ui.Px(d.px(size)), h: ui.Px(d.px(size)), radius: 8, bg: red, cross: ui.CrossCenter, justify: ui.JustifyCenter}, 0xda373cff, label)
	d.icon("phone-off", 16, white)
	id := c.Parent()
	if d.pop() {
		d.voiceLeave()
	}

	rexui.Tooltip(d.k.Kit, id, label)
}

// voiceMuteButtons are the mic and headphones toggles of the dock and the controls bar.
func (d *Discordo) voiceMuteButtons(size float32) {
	listenOnly := voiceListenOnly()
	muted := vc.muted || vc.deafened || listenOnly

	label, iconName := "Mutar microfone", "mic"
	switch {
	case listenOnly:
		label, iconName = "Você está só ouvindo", "mic-off"
	case muted:
		label, iconName = "Ativar microfone", "mic-off"
	}

	if d.voiceControl(iconName, label, size, muted, listenOnly, true) {
		voiceSetMuted(!muted)
	}

	label, iconName = "Ensurdecer", "headphones"
	if vc.deafened {
		label, iconName = "Ativar áudio", "headphone-off"
	}

	if d.voiceControl(iconName, label, size, vc.deafened, false, true) {
		voiceSetDeafened(!vc.deafened)
	}
}

// voicePanel is the call I am in, above the user dock (voice-dock.tsx); nothing when I am in none.
func (d *Discordo) voicePanel() {
	if vc.channel == "" {
		return
	}

	c := d.k.Context()
	st, msg := voiceStatus()

	d.rule(line5, false)
	d.begin("call", box{axis: ui.AxisY, w: ui.Pct(1), bg: surfaceRail})
	d.begin("row", box{axis: ui.AxisX, w: ui.Pct(1), l: 12, r: 12, t: 12, b: 12, gap: 8, cross: ui.CrossCenter})

	d.begin("status", box{axis: ui.AxisY, w: ui.Grow(1), gap: 4})
	d.begin("line", box{axis: ui.AxisX, gap: 6, cross: ui.CrossCenter})

	text, color := "Conectando…", mediaConnecting
	switch st {
	case voice.Connected:
		text, color = "Voz conectada", green
	case voice.Failed:
		text, color = "Falha na chamada", red
	}

	if st == voice.Connecting {
		rexui.Spinner(d.k.Kit, "Conectando", rexui.SpinnerOpts{Size: theme.SizeMini})
	} else {
		d.begin("dot", box{w: ui.Px(d.px(8)), h: ui.Px(d.px(8)), radius: 4, bg: color})
		c.Pop()
	}

	d.text(text, d.ui.s12s, color)
	c.Pop()
	d.text(d.voiceTitle(vc.channel), d.ui.s11, greyple)
	c.Pop()

	if st == voice.Failed {
		if d.ghost("rotate-cw", "Tentar de novo", 16) {
			d.voiceJoin(vc.channel, d.voicePerms(vc.channel))
		}
	} else {
		d.voiceMuteButtons(32)
	}

	d.voiceHangUp(32)
	c.Pop()

	if msg != "" {
		d.begin("error", box{axis: ui.AxisY, w: ui.Pct(1), l: 12, r: 12, b: 12})
		d.para(msg, d.ui.s12, red)
		c.Pop()
	}

	c.Pop()
}

// voicePerms are my permissions in a call's channel; DMs allow everything.
func (d *Discordo) voicePerms(channelID string) int64 {
	for i := range d.st.Guilds {
		if c := d.st.Guilds[i].Channel(channelID); c != nil {
			return c.Permissions
		}
	}

	return permConnect | permSpeak | permStream
}

// voiceView is a voice channel's main area (voice-stage.tsx): the participants' tiles, and joining or the controls.
func (d *Discordo) voiceView(g *Guild, ch *Channel) {
	c := d.k.Context()
	if vc.scr.full && ch.ID == vc.channel {
		return // drawn over the window (voiceFullStage)
	}

	w, _ := c.Size()
	d.voiceStage(g, ch, float32(w)-d.px(railW+sidebarW)-d.px(32))
}

// voiceStage is the stage stageW pixels wide (inside its padding): the grid of tiles, or a focused screen above a
// strip of the others.
func (d *Discordo) voiceStage(g *Guild, ch *Channel, stageW float32) {
	c := d.k.Context()
	inCall := vc.channel == ch.ID
	callers := d.st.Callers(ch.ID)
	items := d.voiceStageItems(callers, inCall)

	var focal *voiceStageItem
	for i := range items {
		if it := &items[i]; it.screen && it.live && it.st.UserID == vc.scr.pinned {
			focal = it
		}
	}

	d.begin("stage", box{axis: ui.AxisY, w: ui.Pct(1), h: ui.Pct(1), bg: surfaceRail, l: 16, r: 16, t: 16, b: 16, gap: 12})

	d.begin("header", box{axis: ui.AxisX, w: ui.Pct(1), gap: 6, cross: ui.CrossCenter})
	d.begin("title", box{axis: ui.AxisY, w: ui.Grow(1)})
	d.text(ch.Name, d.ui.s16b, snow)
	people := "pessoas"
	if len(callers) == 1 {
		people = "pessoa"
	}

	d.text(itoa(len(callers))+" "+people, d.ui.s12, greyple)
	c.Pop()

	if focal != nil && d.voiceStageButton("layout-grid", "Ver todos em grade") {
		vc.scr.pinned = ""
	}

	if focal != nil && focal.watched && d.voiceStageButton("eye-off", "Parar de assistir") {
		voiceWatch(focal.st.UserID, false)
	}

	if inCall {
		label, iconName := "Tela cheia", "maximize-2"
		if vc.scr.full {
			label, iconName = "Sair da tela cheia", "minimize-2"
		}

		if d.voiceStageButton(iconName, label) {
			vc.scr.full = !vc.scr.full
		}
	}
	c.Pop()

	if focal != nil {
		// The focused screen takes the room; the others go in a strip under it.
		d.voiceScreenTile(g, *focal, ui.Pct(1), ui.Grow(1), false, true)

		stripW := d.px(192)
		d.begin("strip", box{axis: ui.AxisX, w: ui.Pct(1), gap: 8, clip: true})
		for _, it := range items {
			switch {
			case it.screen && it.st.UserID == focal.st.UserID:
			case it.screen:
				d.voiceScreenTile(g, it, ui.Px(stripW), ui.Px(stripW*9/16), true, false)
			default:
				d.voiceTile(g, it.st, stripW, inCall)
			}
		}
		c.Pop()
	} else {
		d.voiceGrid(g, items, stageW, inCall)
	}

	d.voiceFooter(ch, "Entrar na chamada", true)
	c.Pop()
}

// voiceFooter ends a call's main area: what went wrong, then joining (the join button) or the controls,
// with the screen share when share.
func (d *Discordo) voiceFooter(ch *Channel, join string, share bool) {
	c := d.k.Context()
	inCall := vc.channel == ch.ID

	msg := ""
	if inCall {
		_, msg = voiceStatus()
		if msg == "" && !vc.scr.dialog {
			msg = vc.scr.err
		}
	} else if vc.endedIn == ch.ID {
		msg = vc.ended
	}

	if msg != "" {
		outline(c, alpha(red, 0.4), 1)
		d.begin("alert", box{axis: ui.AxisX, w: ui.Pct(1), bg: alpha(red, 0.1), radius: 8, l: 16, r: 8, t: 8, b: 8, gap: 8, cross: ui.CrossCenter})
		d.begin("text", box{axis: ui.AxisY, w: ui.Grow(1)})
		d.para(msg, d.ui.s14, red)
		c.Pop()
		if (!inCall || msg == vc.scr.err) && d.secondaryText("Fechar") {
			vc.ended, vc.scr.err = "", ""
		}
		c.Pop()
	}

	d.begin("footer", box{axis: ui.AxisX, w: ui.Pct(1), gap: 8, justify: ui.JustifyCenter, cross: ui.CrossCenter})
	switch {
	case inCall:
		d.voiceMuteButtons(40)
		if share && has(ch.Permissions, permStream) {
			d.voiceShareButton(40)
		}

		if d.voiceControl("settings-2", "Configurações de voz e vídeo", 40, vc.settings, false, false) {
			vc.settings = !vc.settings
		}

		d.voiceHangUp(40)

	case has(ch.Permissions, permConnect):
		d.press(ui.HashString(c.ParentKey(), "join"), box{axis: ui.AxisX, h: ui.Px(d.px(44)), l: 24, r: 24, radius: 8, bg: blurple, gap: 8, cross: ui.CrossCenter}, 0x4752c4ff, join)
		d.icon("phone-call", 16, white)
		d.text(join, d.ui.s14m, white)
		if d.pop() {
			d.voiceJoin(ch.ID, ch.Permissions)
		}

	default:
		d.text("Você não tem permissão para entrar nesta chamada.", d.ui.s14, greyple)
	}
	c.Pop()
}

// voiceGrid is the tiles in columns of at least 16rem (grid-cols auto-fit), centred in the height.
func (d *Discordo) voiceGrid(g *Guild, items []voiceStageItem, stageW float32, inCall bool) {
	k, c := d.k, d.k.Context()
	_, h := c.Size()
	gap := d.px(12)

	k.NextSize(ui.Pct(1), ui.Grow(1))
	v := k.BeginScroll("tiles", &vc.scroll, base.ScrollVertical)
	d.begin("grid", box{axis: ui.AxisY, w: ui.Pct(1), gap: 12, justify: ui.JustifyCenter})

	if len(items) == 0 {
		d.begin("nobody", box{axis: ui.AxisY, w: ui.Pct(1), h: ui.Px(float32(h) / 2), cross: ui.CrossCenter, justify: ui.JustifyCenter})
		d.text("Ninguém na chamada ainda.", d.ui.s14, greyple)
		c.Pop()
	}

	cols := max(1, min(len(items), int((stageW+gap)/(d.px(256)+gap))))
	tileW := (stageW - gap*float32(cols-1)) / float32(cols)

	for i := 0; i < len(items); i += cols {
		d.begin("row "+itoa(i), box{axis: ui.AxisX, w: ui.Pct(1), gap: 12, justify: ui.JustifyCenter})
		for _, it := range items[i:min(i+cols, len(items))] {
			if it.screen {
				d.voiceScreenTile(g, it, ui.Px(tileW), ui.Px(tileW*9/16), false, false)
			} else {
				d.voiceTile(g, it.st, tileW, inCall)
			}
		}
		c.Pop()
	}

	c.Pop()
	k.CloseScrollContent(&v)
	rexui.ScrollBars(k.Kit, &v)
	k.CloseScroll(&v)
}

// voiceVolumes are the tile menu's choices, in percent.
var voiceVolumes = [...]int{0, 25, 50, 75, 100, 150, 200}

// voiceTile is voice-stage.tsx's Tile for a person: the avatar with the speaking ring, the name with mute and deafen.
func (d *Discordo) voiceTile(g *Guild, st VoiceState, w float32, inCall bool) {
	c := d.k.Context()
	name, u := d.voiceName(g, st.UserID), User{ID: st.UserID}
	if g != nil {
		for _, m := range g.Members {
			if m.User.ID == st.UserID {
				u = m.User
			}
		}
	} else if dm := d.st.DM(vc.channel); dm != nil {
		for _, r := range dm.Recipients {
			if r.ID == st.UserID {
				u = r
			}
		}
	}

	if st.UserID == d.st.Me.ID {
		u = d.st.Me
	}

	// Someone's camera: asked for on a click (as the web's tiles), then drawn over the tile.
	own := st.UserID == d.st.Me.ID
	camera := inCall && !own && vc.s != nil && slices.Contains(st.Tracks, voice.TrackCamera)
	watched := camera && vc.scr.cams[st.UserID]
	if !camera && vc.scr.cams[st.UserID] {
		delete(vc.scr.cams, st.UserID) // they turned it off: seeing it again is a new choice
	}

	// Hoverable, so a right click reaches it.
	flags := d.style(box{axis: ui.AxisY, w: ui.Px(w), h: ui.Px(w * 9 / 16), radius: 12, bg: surfaceRaised, clip: true, cross: ui.CrossCenter, justify: ui.JustifyCenter})
	c.NextSemName(name)
	id := c.BoxKeyed(flags|d.k.InteractFlags(base.HoverOn), ui.HashString(c.ParentKey(), st.UserID), nil)
	c.Push(id)

	// The ring (outline 2px + offset 3px in the web) stays in the layout when silent, so nothing moves.
	ring := uint32(0)
	if inCall && d.voiceSpeaking(st.UserID) {
		ring = mediaSpeaking
	}

	size := min(float32(80), w/d.ui.scale/4)
	if tex, pw, ph := d.voiceTexture(voiceFrame(st.UserID, voice.TrackCamera)); watched && tex != 0 {
		d.k.NextSize(ui.Pct(1), ui.Pct(1))
		d.k.ExternalImage(2, tex, pw, ph, ui.FitFill, d.px(12), "Câmera de "+name)
	} else {
		outline(c, ring, d.px(3))
		d.begin("ring", box{radius: size/2 + 6, l: 6, r: 6, t: 6, b: 6})
		d.avatar(u, name, size, rexui.PresenceNone, surfaceRaised)
		c.Pop()
	}

	if camera {
		label, glyph := "Ver câmera de "+name, "video"
		if watched {
			label, glyph = "Parar de ver a câmera", "video-off"
		}

		c.NextAnchor(ui.AnchorTopRight, d.px(8), d.px(8))
		d.begin("camera", box{})
		if d.voiceStageButton(glyph, label) {
			if vc.scr.cams == nil {
				vc.scr.cams = map[string]bool{}
			}

			vc.scr.cams[st.UserID] = !watched
			vc.s.Watch(st.UserID, voice.TrackCamera, !watched)
		}
		c.Pop()
	}

	c.NextAnchor(ui.AnchorBottomLeft, d.px(8), d.px(8))
	d.begin("name", box{axis: ui.AxisX, l: 8, r: 8, t: 4, b: 4, gap: 6, radius: 6, bg: 0x000000a6, cross: ui.CrossCenter})
	if st.Muted {
		d.icon("mic-off", 12, mediaMuted)
	}

	if st.Deafened {
		d.icon("headphone-off", 12, mediaMuted)
	}

	d.text(name, d.ui.s12s, white)
	c.Pop()
	c.Pop()

	// The volume of someone else, on a right click (the web's tile menu).
	if !inCall || vc.s == nil || st.UserID == d.st.Me.ID {
		return
	}

	cur := 100
	if v, ok := vc.volumes[st.UserID]; ok {
		cur = int(v*100 + 0.5)
	}

	item, ok := rexui.ContextMenu(d.k.Kit, id, func(items *[]ui.MenuItem) {
		*items = append(*items, ui.MenuItem{Title: "Volume de " + name, Header: true})
		for _, p := range voiceVolumes {
			mark := ui.MarkNone
			if p == cur {
				mark = ui.MarkOn
			}

			*items = append(*items, ui.MenuItem{ID: p, Title: itoa(p) + "%", Mark: mark})
		}
	})

	if ok {
		if vc.volumes == nil {
			vc.volumes = map[string]float32{}
		}

		vc.volumes[st.UserID] = float32(item) / 100
		d.voiceApplyVolume(st.UserID)
	}
}

// voiceDialogs draws the call's dialogs (devices) and follows the session between frames.
func (d *Discordo) voiceDialogs() {
	if vc.channel != "" && (d.gen != vc.gen || d.phase != phaseOnline) {
		d.voiceLeave() // signed out
	}

	// Ended by itself (another window took the call): the dock goes, the stage says why.
	if vc.s != nil {
		if st, msg := vc.s.Status(); st == voice.Ended {
			vc.ended, vc.endedIn = msg, vc.channel
			vc.s, vc.channel = nil, ""
			voiceScreenReset()
		}
	}

	d.worldSync()

	if vc.channel == "" {
		vc.settings = false
	}

	dlg := rexui.BeginDialog(d.k.Kit, "voice settings", &vc.settings, rexui.DialogOpts{Title: "Voz e vídeo", Width: 480, DimCancels: true})
	if dlg.Visible {
		d.para("Dispositivos do microfone e da saída. Tudo vale na hora, inclusive na chamada.", d.ui.s14, fog)
		d.begin("gap", box{t: 12})
		d.k.Context().Pop()
		d.voiceSettingsSection()
	}

	rexui.EndDialog(d.k.Kit, &dlg)
	d.voiceShareDialog()
	d.voiceFullStage()

	// The device list is read again each time a settings page opens (devices come and go).
	if !vc.devs.drawn {
		vc.devs.loaded = false
	}

	vc.devs.drawn = false
}

// voiceSettingsSection is the user settings' "Voz e vídeo" section (device-settings-dialog.tsx's Voz).
func (d *Discordo) voiceSettingsSection() {
	c := d.k.Context()
	vc.devs.drawn = true

	if !vc.devs.loaded && !vc.devs.loading {
		vc.devs.loading = true
		go func() {
			ins, errIn := voice.Inputs()
			outs, errOut := voice.Outputs()
			d.post(func(*Discordo) {
				vc.devs = voiceDevices{loaded: true, inputs: ins, outputs: outs, drawn: true}
				vc.devErr = ""
				if err := errors.Join(errIn, errOut); err != nil {
					vc.devErr = "Não foi possível listar os dispositivos: " + err.Error()
				}
			})
		}()
	}

	outline(c, line10, 1)
	d.begin("voz", box{axis: ui.AxisY, w: ui.Pct(1), bg: surfaceRaised, radius: 12, l: 16, r: 16, t: 16, b: 16, gap: 20})

	d.begin("title", box{axis: ui.AxisX, gap: 8, cross: ui.CrossCenter})
	d.icon("mic", 16, blurpleSoft)
	d.text("Voz", d.ui.s16b, snow)
	c.Pop()

	if d.voiceDeviceSelect("Microfone", vc.devs.inputs, &vc.input) {
		d.voiceApplyDevices()
	}

	// MicLevel: what the others hear, while in a call.
	level := float32(0)
	if vc.s != nil {
		if l := vc.s.MicLevel(); l > 0.01 && !vc.muted && !vc.deafened {
			level = min(1, 0.3+l*5)
		}

		c.RequestFrame()
	}

	d.begin("meter", box{axis: ui.AxisY, w: ui.Pct(1), gap: 6})
	d.begin("track", box{axis: ui.AxisX, w: ui.Pct(1), h: ui.Px(d.px(8)), radius: 4, bg: surfaceActive, clip: true})
	c.NextSize(ui.Pct(level), ui.Pct(1))
	d.begin("fill", box{radius: 4, bg: green})
	c.Pop()
	c.Pop()

	hint := "Entre numa chamada para testar o microfone."
	if vc.s != nil {
		hint = "Fale para testar: a barra mostra o que os outros ouvem."
	}

	d.text(hint, d.ui.s12, greyple)
	c.Pop()

	if d.voiceDeviceSelect("Saída de áudio", vc.devs.outputs, &vc.output) {
		d.voiceApplyDevices()
	}

	if vc.devErr != "" {
		d.para(vc.devErr, d.ui.s12, red)
	}

	c.Pop()

	d.begin("gap", box{t: 12})
	c.Pop()
	d.para("A câmera ainda não está no app nativo: use o Discordo na web para ligar a sua.", d.ui.s12, greyple)
}

// voiceDeviceSelect is DeviceSelect: "Padrão do sistema" and the devices; it reports a change of *id.
func (d *Discordo) voiceDeviceSelect(label string, devs []voice.Device, id *string) bool {
	d.begin("field "+label, box{axis: ui.AxisY, w: ui.Pct(1), gap: 8})
	d.text(strings.ToUpper(label), d.ui.caption, greyple)

	options := []string{"Padrão do sistema"}
	sel := 0
	for i, dev := range devs {
		options = append(options, dev.Name)
		if dev.ID == *id {
			sel = i + 1
		}
	}

	d.k.NextWidth(ui.Pct(1))
	changed := d.k.Select(label, &sel, options).Changed
	if changed {
		*id = ""
		if sel > 0 {
			*id = devs[sel-1].ID
		}
	}

	d.k.Context().Pop()

	return changed
}

// voiceApplyDevices moves the call to the chosen devices.
func (d *Discordo) voiceApplyDevices() {
	s, in, out := vc.s, vc.input, vc.output
	if s == nil {
		return
	}

	go func() {
		err := s.SetDevices(in, out)
		d.post(func(*Discordo) {
			vc.devErr = ""
			if err != nil {
				vc.devErr = "Não foi possível trocar o dispositivo: " + err.Error()
			}
		})
	}()
}
