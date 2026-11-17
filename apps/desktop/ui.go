package main

// The window: login or the workspace (features/shell/workspace.tsx), and where each area is drawn.

import (
	"gonativeui/rexui"
	"gonativeui/theme"
	"gonativeui/ui"
	"gonativeui/widget/base"
)

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
		d.themeBackdrop(W, H-top)
		d.workspace(W, H-top)
	} else {
		d.login(W, H-top)
	}

	c.Pop()
	// Each area draws its own dialogs and overlays, above everything.
	d.chatDialogs()
	d.spacesDialogs()
	d.socialDialogs()
	d.identityDialogs()
	d.voiceDialogs()
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
		d.voiceView(g, ch)
	case ch != nil:
		d.chat(ch.Name, ch.Topic, true, ch.Permissions, mainW, H)
	default:
		if dm := d.st.DM(d.channel); dm != nil {
			d.chat(d.st.DMName(*dm), "", false, permSend|permReact|permAttach, mainW, H)
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

	outline(c, line10, 1)
	d.begin("card", box{axis: ui.AxisY, w: ui.Px(d.px(448)), bg: surfaceSidebar, radius: 16, l: 32, r: 32, t: 32, b: 32, gap: 8, cross: ui.CrossCenter})

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
		outline(c, alpha(red, 0.4), 1)
		d.begin("error", box{axis: ui.AxisY, w: ui.Pct(1), bg: alpha(red, 0.1), radius: 8, l: 16, r: 16, t: 12, b: 12})
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
