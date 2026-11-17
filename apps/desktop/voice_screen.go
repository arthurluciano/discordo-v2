package main

// Screens in a call: watching someone's (voice-stage.tsx's screen tiles, focus and full screen) and sharing mine
// (share-dialog.tsx). Package voice captures, encodes and decodes; this is the window's side.

import (
	"errors"
	"slices"

	"discordo/desktop/voice"

	"gonativeui/rexui"
	"gonativeui/theme"
	"gonativeui/ui"
	"gonativeui/widget/base"
)

// voiceResolutions are lib/media/presets.ts's RESOLUTIONS.
var voiceResolutions = [...]struct {
	label string
	w, h  int
	mbps  float64
}{
	{"480p", 854, 480, 1.5},
	{"720p", 1280, 720, 3},
	{"1080p", 1920, 1080, 6},
	{"1440p", 2560, 1440, 10},
	{"4K", 3840, 2160, 20},
}

// voiceScreens is the call's screens: the share dialog, my preset, what I watch, the focused tile.
type voiceScreens struct {
	dialog, wasOpen bool
	res, fps        int  // the preset (zero: DEFAULT_SCREEN_PRESET, 1080p at 30)
	silent          int  // 1: without the sound (a Select's index; the web's default is with it)
	starting        bool // StartScreen on its way
	err             string

	watching map[string]bool // by user: their screen is asked for
	pinned   string          // the focused screen, by user
	full     bool            // the stage covers the window
}

// voiceStageItem is a tile of the stage: someone, or someone's screen.
type voiceStageItem struct {
	st      VoiceState
	screen  bool
	own     bool
	live    bool // a screen that is received (or mine, going out)
	watched bool
	inCall  bool // mine: "Assistir" can ask for it
}

// voiceFrame is someone's latest picture of a track, retained (release it); the offscreen test draws its own.
var voiceFrame = func(userID, track string) voice.Picture {
	if vc.s == nil {
		return 0
	}

	return vc.s.Frame(userID, track)
}

// voicePreset is the share's options and bitrate (presets.ts's screenEncoding: 60 fps gets 1.5x).
func voicePreset() (voice.ScreenOptions, int) {
	if vc.scr.fps == 0 {
		vc.scr.res, vc.scr.fps = 2, 30
	}

	r := voiceResolutions[vc.scr.res]
	bitrate := r.mbps * 1e6
	if vc.scr.fps == 60 {
		bitrate *= 1.5
	}

	return voice.ScreenOptions{MaxWidth: r.w, MaxHeight: r.h, FPS: vc.scr.fps, Sound: vc.scr.silent == 0}, int(bitrate)
}

// voiceScreenReset forgets the screens of a call that ended.
func voiceScreenReset() {
	vc.scr.watching, vc.scr.pinned, vc.scr.full, vc.scr.dialog, vc.scr.starting = nil, "", false, false, false
}

// voiceStageItems are the stage's tiles, each person after their screen (voice-stage.tsx's tiles).
func (d *Discordo) voiceStageItems(callers []VoiceState, inCall bool) []voiceStageItem {
	var items []voiceStageItem
	for _, st := range callers {
		own := st.UserID == d.st.Me.ID
		published := slices.Contains(st.Tracks, voice.TrackScreen)
		if own && inCall {
			published = vc.s != nil && vc.s.Sharing()
		}

		watched := !own && inCall && published && vc.scr.watching[st.UserID]
		if !published && vc.scr.watching[st.UserID] {
			delete(vc.scr.watching, st.UserID) // they stopped: watching again is a new choice
			if vc.s != nil {
				vc.s.Watch(st.UserID, voice.TrackScreen, false)
			}
		}

		if published {
			items = append(items, voiceStageItem{st: st, screen: true, own: own, watched: watched, live: watched || (own && inCall), inCall: inCall})
		}

		items = append(items, voiceStageItem{st: st, own: own})
	}

	return items
}

// voiceWatch starts or stops receiving someone's screen, focusing it as the web does.
func voiceWatch(userID string, on bool) {
	if vc.s == nil {
		return
	}

	if vc.scr.watching == nil {
		vc.scr.watching = map[string]bool{}
	}

	vc.scr.watching[userID] = on
	vc.s.Watch(userID, voice.TrackScreen, on)
	vc.scr.pinned = ""
	if on {
		vc.scr.pinned = userID
	}
}

// voiceStageButton is the stage header's StageButton: an outline icon button on black.
func (d *Discordo) voiceStageButton(iconName, label string) bool {
	c := d.k.Context()
	outline(c, line10, 1)
	d.press(ui.HashString(c.ParentKey(), label), box{w: ui.Px(d.px(32)), h: ui.Px(d.px(32)), radius: 8, bg: 0x00000099, cross: ui.CrossCenter, justify: ui.JustifyCenter}, 0x000000cc, label)
	d.icon(iconName, 16, white)
	id := c.Parent()
	clicked := d.pop()
	rexui.Tooltip(d.k.Kit, id, label)

	return clicked
}

// voiceScreenTile is a screen's tile: the picture (or the monitor glyph and "Assistir"), the "Tela" badge, the name.
func (d *Discordo) voiceScreenTile(g *Guild, it voiceStageItem, w, h ui.Size, strip, focal bool) {
	k, c := d.k, d.k.Context()
	name := d.voiceName(g, it.st.UserID)

	var pic voice.Picture
	note := ""
	if it.live {
		pic = voiceFrame(it.st.UserID, voice.TrackScreen)
	}

	if it.live && vc.s != nil {
		note = vc.s.VideoNote(it.st.UserID, voice.TrackScreen)
	}

	radius := float32(12)
	if strip {
		radius = 8
	}

	b := box{axis: ui.AxisY, w: w, h: h, radius: radius, bg: surfaceRaised, clip: true, cross: ui.CrossCenter, justify: ui.JustifyCenter, gap: 8}
	if it.live {
		b.bg = 0x000000ff
	}

	// A received screen focuses on a click (the web's cover button); one that is not shows "Assistir".
	key := ui.HashString(c.ParentKey(), "screen "+it.st.UserID)
	clickable := it.live && !focal
	if clickable {
		d.press(key, b, 0, "Focar "+name+", compartilhando tela")
	} else {
		c.NextSemName(name + ", compartilhando tela")
		c.Push(c.BoxKeyed(d.style(b), key, nil))
	}

	if tex, pw, ph := d.voiceTexture(pic); tex != 0 {
		k.NextSize(ui.Pct(1), ui.Pct(1))
		k.ExternalImage(1, tex, pw, ph, ui.FitContain, d.px(radius), "Tela de "+name)
	} else if !strip || it.live || it.own || !it.inCall { // a strip tile has room for "Assistir" alone
		d.icon("monitor-up", 40, fog)
		if note != "" {
			d.begin("note", box{axis: ui.AxisY, w: ui.Pct(0.8)})
			d.para(note, d.ui.s12, fog)
			c.Pop()
		}
	}

	// The badge: blurple while the picture is there.
	badgeBG, badgeFG := uint32(0x000000a6), fog
	if it.live {
		badgeBG, badgeFG = blurple, white
	}

	c.NextAnchor(ui.AnchorTopLeft, d.px(8), d.px(8))
	d.begin("badge", box{axis: ui.AxisX, l: 6, r: 6, t: 2, b: 2, gap: 4, radius: 6, bg: badgeBG, cross: ui.CrossCenter})
	d.icon("monitor-up", 12, badgeFG)
	d.text("TELA", d.ui.caption, badgeFG)
	c.Pop()

	if !it.own && !it.live && it.inCall {
		c.NextAnchor(ui.AnchorBottom, 0, d.px(40))
		if strip {
			c.NextCenter()
		}

		d.press(ui.HashString(c.ParentKey(), "watch"), box{axis: ui.AxisX, h: ui.Px(d.px(32)), l: 12, r: 12, radius: 8, bg: blurple, gap: 6, cross: ui.CrossCenter}, 0x4752c4ff, "Assistir")
		d.icon("eye", 16, white)
		d.text("Assistir", d.ui.s14m, white)
		if d.pop() {
			voiceWatch(it.st.UserID, true)
		}
	}

	c.NextAnchor(ui.AnchorBottomLeft, d.px(8), d.px(8))
	d.begin("name", box{axis: ui.AxisX, l: 8, r: 8, t: 4, b: 4, radius: 6, bg: 0x000000a6, cross: ui.CrossCenter})
	d.text(name, d.ui.s12s, white)
	c.Pop()

	if clickable {
		if d.pop() {
			vc.scr.pinned = it.st.UserID
		}
	} else {
		c.Pop()
	}

	// Frames come from the network: the next one wakes the app (Session's OnChange).
}

// voiceName is someone's name in a call: their member name, or mine.
func (d *Discordo) voiceName(g *Guild, userID string) string {
	if userID == d.st.Me.ID {
		return d.st.Me.Name()
	}

	if g != nil {
		for _, m := range g.Members {
			if m.User.ID == userID {
				return m.Name()
			}
		}
	}

	return "Alguém"
}

// voiceShareButton is the controls' "Compartilhar tela": blurple while sharing.
func (d *Discordo) voiceShareButton(size float32) {
	sharing := vc.s != nil && vc.s.Sharing()
	label := "Compartilhar tela"
	if sharing {
		label = "Transmitindo a tela"
	}

	c := d.k.Context()
	bg, hover := uint32(0), uint32(surfaceHover)
	if sharing {
		bg, hover = blurple, 0x4752c4ff
	}

	outline(c, line10, 1)
	d.press(ui.HashString(c.ParentKey(), "share"), box{w: ui.Px(d.px(size)), h: ui.Px(d.px(size)), radius: 8, bg: bg, cross: ui.CrossCenter, justify: ui.JustifyCenter}, hover, label)
	d.icon("monitor-up", 16, snow)
	id := c.Parent()
	if d.pop() {
		vc.scr.dialog, vc.scr.err = true, ""
	}

	rexui.Tooltip(d.k.Kit, id, label)
}

// voiceButton is a dialog button: an optional icon and a label on bg.
func (d *Discordo) voiceButton(iconName, label string, bg, hover, fg uint32, disabled bool) bool {
	c := d.k.Context()
	if disabled {
		hover, fg = 0, alpha(fg, 0.5)
	}

	d.press(ui.HashString(c.ParentKey(), label), box{axis: ui.AxisX, h: ui.Px(d.px(36)), l: 14, r: 14, radius: 8, bg: bg, gap: 8, cross: ui.CrossCenter}, hover, label)
	if iconName != "" {
		d.icon(iconName, 16, fg)
	}

	d.text(label, d.ui.s14m, fg)

	return d.pop() && !disabled
}

// voicePick opens the system's picker on screens or windows; the pick shows in the dialog's preview.
func (d *Discordo) voicePick(window bool) {
	s := vc.s
	if s == nil {
		return
	}

	opt, _ := voicePreset()
	opt.Window = window
	vc.scr.err = ""

	err := s.PickScreen(opt, func(err error) {
		if err == nil || errors.Is(err, voice.ErrPickerCancelled) {
			return
		}

		d.post(func(*Discordo) {
			if vc.s == s {
				vc.scr.err = "A captura da tela parou: " + err.Error()
			}
		})
	})
	if err != nil {
		vc.scr.err = err.Error()
	}
}

// voiceGoLive sends the previewed screen ("Transmitir").
func (d *Discordo) voiceGoLive() {
	s := vc.s
	opt, bitrate := voicePreset()
	vc.scr.starting, vc.scr.dialog = true, false

	go func() {
		err := s.StartScreen(opt.FPS, bitrate)
		if err != nil {
			_ = s.StopScreen()
		}

		d.post(func(*Discordo) {
			vc.scr.starting = false
			if err != nil && vc.s == s {
				vc.scr.err = "Não foi possível transmitir a tela: " + err.Error()
			}
		})
	}()
}

// voiceStopShare ends my share ("Parar transmissão"), or drops a preview that never went out.
func voiceStopShare() {
	if s := vc.s; s != nil {
		go func() { _ = s.StopScreen() }()
	}
}

// voiceShareDialog is share-dialog.tsx: pick a screen or a window, see it, tune it, go live; or stop.
func (d *Discordo) voiceShareDialog() {
	k, c := d.k, d.k.Context()
	if vc.channel == "" {
		vc.scr.dialog = false
	}

	// Closing the dialog drops a preview that never went out.
	if vc.scr.wasOpen && !vc.scr.dialog && vc.s != nil && !vc.s.Sharing() && !vc.scr.starting {
		voiceStopShare()
	}

	vc.scr.wasOpen = vc.scr.dialog

	live := vc.s != nil && vc.s.Sharing()
	title, desc := "Compartilhar tela", "Escolha o que mostrar, confira a prévia e comece quando estiver pronto."
	if live {
		title, desc = "Transmitindo a tela", "Troque a fonte ou ajuste a qualidade: quem assiste vê a mudança na hora."
	}

	dlg := rexui.BeginDialog(k.Kit, "share screen", &vc.scr.dialog, rexui.DialogOpts{Title: title, Width: 720, DimCancels: true})
	if !dlg.Visible {
		rexui.EndDialog(k.Kit, &dlg)
		return
	}

	d.para(desc, d.ui.s14, fog)
	d.begin("gap", box{t: 4})
	c.Pop()

	if voiceScreenUnsupported != "" {
		d.para(voiceScreenUnsupported, d.ui.s14, red)
		rexui.EndDialog(k.Kit, &dlg)
		return
	}

	d.begin("sources", box{axis: ui.AxisX, w: ui.Pct(1), gap: 12})
	for _, src := range []struct{ icon, label, desc string }{
		{"monitor", "Tela inteira", "Tudo o que aparece num monitor."},
		{"app-window", "Janela", "Um aplicativo, mesmo atrás de outros."},
	} {
		outline(c, line10, 1)
		hot := d.press(ui.HashString(c.ParentKey(), src.label), box{axis: ui.AxisX, w: ui.Grow(1), l: 16, r: 16, t: 16, b: 16, gap: 12, radius: 12, bg: surfaceRaised, cross: ui.CrossCenter}, surfaceHover, src.label)
		iconColor := uint32(fog)
		if hot {
			iconColor = snow
		}

		d.begin("glyph", box{w: ui.Px(d.px(44)), h: ui.Px(d.px(44)), radius: 8, bg: surfaceActive, cross: ui.CrossCenter, justify: ui.JustifyCenter})
		d.icon(src.icon, 20, iconColor)
		c.Pop()

		d.begin("text", box{axis: ui.AxisY, w: ui.Grow(1), gap: 2})
		d.text(src.label, d.ui.s14m, snow)
		d.text(src.desc, d.ui.s12, greyple)
		c.Pop()

		if d.pop() {
			d.voicePick(src.label == "Janela")
		}
	}
	c.Pop()

	// The preview: what goes out (or would), before anyone sees it.
	pic := voiceFrame(d.st.Me.ID, voice.TrackScreen)

	outline(c, line10, 1)
	d.begin("preview", box{axis: ui.AxisY, w: ui.Pct(1), h: ui.Px(d.px(330)), radius: 12, bg: 0x000000ff, clip: true, cross: ui.CrossCenter, justify: ui.JustifyCenter, gap: 8})
	tex, pw, ph := d.voiceTexture(pic)
	if tex != 0 {
		k.NextSize(ui.Pct(1), ui.Pct(1))
		k.ExternalImage(1, tex, pw, ph, ui.FitContain, 0, "Prévia da tela")

		c.NextAnchor(ui.AnchorTopLeft, d.px(12), d.px(12))
		if live {
			d.begin("live", box{axis: ui.AxisX, l: 10, r: 10, t: 4, b: 4, gap: 6, radius: 12, bg: red, cross: ui.CrossCenter})
			d.icon("radio", 14, white)
			d.text("AO VIVO", d.ui.s12s, white)
		} else {
			d.begin("preview note", box{axis: ui.AxisX, l: 10, r: 10, t: 4, b: 4, radius: 12, bg: 0x000000b3, cross: ui.CrossCenter})
			d.text("Prévia: ninguém está vendo ainda", d.ui.s12s, white)
		}
		c.Pop()
	} else {
		d.icon("monitor-up", 32, greyple)
		d.text("Escolha acima o que compartilhar. A prévia aparece aqui antes de alguém ver.", d.ui.s14, greyple)
	}
	c.Pop()

	// The quality: resolution and frame rate (the codec is H.264, on the GPU).
	d.begin("quality", box{axis: ui.AxisX, w: ui.Pct(1), gap: 16})

	d.begin("resolution", box{axis: ui.AxisY, w: ui.Grow(1), gap: 8})
	d.text("RESOLUÇÃO", d.ui.caption, greyple)
	voicePreset()
	options := make([]string, len(voiceResolutions))
	for i, r := range voiceResolutions {
		options[i] = r.label
	}

	k.NextWidth(ui.Pct(1))
	changed := k.Select("Resolução", &vc.scr.res, options).Changed
	c.Pop()

	d.begin("fps", box{axis: ui.AxisY, w: ui.Grow(1.4), gap: 8})
	d.text("QUADROS POR SEGUNDO", d.ui.caption, greyple)
	d.begin("choices", box{axis: ui.AxisX, gap: 4})
	for _, fps := range []int{30, 60} {
		bg := uint32(0)
		if vc.scr.fps == fps {
			bg = surfaceActive
		}

		if d.voiceButton("", itoa(fps)+" fps", bg, surfaceHover, snow, false) && vc.scr.fps != fps {
			vc.scr.fps, changed = fps, true
		}
	}
	c.Pop()
	c.Pop()

	d.begin("codec", box{axis: ui.AxisY, w: ui.Grow(1), gap: 8})
	d.text("CODEC", d.ui.caption, greyple)
	d.begin("value", box{axis: ui.AxisX, h: ui.Px(d.px(36)), cross: ui.CrossCenter})
	d.text("H.264 (GPU)", d.ui.s14, snow)
	c.Pop()
	c.Pop()

	d.begin("sound", box{axis: ui.AxisY, w: ui.Grow(1), gap: 8})
	d.text("ÁUDIO", d.ui.caption, greyple)
	k.NextWidth(ui.Pct(1))
	if k.Select("Áudio", &vc.scr.silent, []string{"Com som", "Sem som"}).Changed {
		changed = true
	}
	c.Pop()

	c.Pop()

	hint := "Janela: o som do aplicativo dela. Tela inteira: tudo o que toca no computador. O som da chamada fica de fora."
	if vc.scr.silent == 1 {
		hint = "Só a imagem."
	}

	d.para(hint, d.ui.s12, greyple)

	if changed && vc.s != nil {
		vc.s.SetScreenOptions(voicePreset())
	}

	if vc.scr.err != "" {
		d.para(vc.scr.err, d.ui.s14, red)
	}

	d.begin("actions", box{axis: ui.AxisX, w: ui.Pct(1), gap: 8, justify: ui.JustifyEnd})
	if live {
		if d.voiceButton("square", "Parar transmissão", red, 0xda373cff, white, false) {
			voiceStopShare()
			vc.scr.dialog = false
		}
	} else {
		if d.voiceButton("", "Cancelar", 0, surfaceHover, snow, false) {
			vc.scr.dialog = false
		}

		if d.voiceButton("monitor-up", "Transmitir", blurple, 0x4752c4ff, white, tex == 0 || vc.scr.starting) {
			d.voiceGoLive()
		}
	}
	c.Pop()

	rexui.EndDialog(k.Kit, &dlg)
}

// voiceFullStage draws the stage over the whole window (the web's full screen); it ends with the call or the channel.
func (d *Discordo) voiceFullStage() {
	if !vc.scr.full {
		return
	}

	g := d.st.Guild(d.guild)
	var ch *Channel
	if g != nil {
		ch = g.Channel(d.channel)
	}

	if ch == nil || ch.Type != ChannelVoice || ch.ID != vc.channel || d.phase != phaseOnline {
		vc.scr.full = false
		return
	}

	c := d.k.Context()
	w, h := c.Size()
	top := float32(0)
	if !c.NativeMenus() {
		top = theme.Snap(d.k.Theme().Px.ControlH)
	}

	theme.At(c, 0, top, float32(w), float32(h)-top)
	c.Push(c.Box(base.Floating, "full stage"))
	d.voiceStage(g, ch, float32(w)-d.px(32))
	c.Pop()
}
