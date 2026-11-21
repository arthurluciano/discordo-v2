package main

// Servers: the rail (server-rail.tsx), the guild sidebar (guild-sidebar.tsx) and their dialogs
// (guild-dialogs.tsx here; channel-settings-dialog.tsx in spaces_channel.go, server-settings-dialog.tsx in
// spaces_settings.go).

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"time"

	"gonativeui/img"
	"gonativeui/platform"
	"gonativeui/rexui"
	"gonativeui/ui"
	"gonativeui/widget/base"
)

// spaceState is the servers area's UI state. main.go is shared, so it lives here, one per window.
type spaceState struct {
	pending string // a guild just created or joined: opened once the gateway brings it
	err     string // the last failure, shown in the open dialog (the web's FieldError)

	adding          bool
	newName, invite ui.TextBuffer

	inviting      bool
	expiry, uses  int // indexes in inviteExpiry and inviteUses
	link, copied  string
	creating      bool
	channelType   int
	channelName   ui.TextBuffer
	leaving       bool
	channel       channelSettings
	server        serverSettings
	inviteGuildID string
}

var spaceStates = map[*Discordo]*spaceState{}

func (d *Discordo) spaces() *spaceState {
	s := spaceStates[d]
	if s == nil {
		s = &spaceState{expiry: 3}
		spaceStates[d] = s
	}

	return s
}

// inviteExpiry is logic.ts's INVITE_EXPIRY.
var inviteExpiry = [...]struct {
	label   string
	seconds int
}{{"Nunca", 0}, {"30 minutos", 30 * 60}, {"1 hora", 60 * 60}, {"1 dia", 24 * 60 * 60}, {"7 dias", 7 * 24 * 60 * 60}}

var (
	inviteUses        = [...]int{0, 1, 5, 10, 25, 50, 100}
	inviteExpiryNames = func() (l []string) {
		for _, e := range inviteExpiry {
			l = append(l, e.label)
		}

		return l
	}()
	inviteUseNames = func() (l []string) {
		for _, n := range inviteUses {
			switch n {
			case 0:
				l = append(l, "Sem limite")
			case 1:
				l = append(l, "1 uso")
			default:
				l = append(l, itoa(n)+" usos")
			}
		}

		return l
	}()
)

var (
	inviteFromLink = regexp.MustCompile(`/invite/([\w-]+)/?(?:[?#].*)?$`)
	inviteBare     = regexp.MustCompile(`^[\w-]+$`)
)

// parseInviteCode is logic.ts's: a bare code or any link ending in /invite/CODE; "" when it is neither.
func parseInviteCode(input string) string {
	text := strings.TrimSpace(input)
	if m := inviteFromLink.FindStringSubmatch(text); m != nil {
		return m[1]
	}

	if inviteBare.MatchString(text) {
		return text
	}

	return ""
}

// joinInvite uses an invite (a link in a message, or one the app was opened with) and opens its server.
func (d *Discordo) joinInvite(code string) {
	client, gen := d.client, d.gen
	go func() {
		var g Guild
		err := client.do(context.Background(), "POST", "/invites/"+code, nil, &g)

		d.post(func(d *Discordo) {
			if d.gen != gen {
				return
			}

			if err != nil {
				d.status = "Não foi possível usar o convite: " + err.Error()
				return
			}

			d.spaces().pending = g.ID // opened as soon as the gateway has it
		})
	}()
}

func (d *Discordo) inviteLink(code string) string { return d.client.Server + "/invite/" + code }

// guildCopy is lib/use-copy.ts: copies text; "Copiado" shows for two seconds.
func (d *Discordo) guildCopy(text string) {
	s := d.spaces()
	platform.SetClipboardText([]byte(text))
	s.copied = text

	go func() {
		time.Sleep(2 * time.Second)
		d.post(func(*Discordo) {
			if s.copied == text {
				s.copied = ""
			}
		})
	}()
}

// myTop is lib/permissions.ts's topPosition: the highest role position a user holds; the owner outranks all.
func myTop(g *Guild, userID string) int {
	if g.OwnerID == userID {
		return int(^uint(0) >> 1)
	}

	top := 0
	if i := slices.IndexFunc(g.Members, func(m Member) bool { return m.User.ID == userID }); i >= 0 {
		for _, r := range g.Roles {
			if slices.Contains(g.Members[i].Roles, r.ID) {
				top = max(top, r.Position)
			}
		}
	}

	return top
}

// rail is features/spaces/server-rail.tsx.
func (d *Discordo) rail() {
	c := d.k.Context()

	if d.railButton("home", "Inicial", d.guild == "", func(size int32) {
		h, _ := d.mascot(size)
		c.NextSize(ui.Pct(1), ui.Pct(1))
		d.k.Image(1, h, ui.FitFill, d.ui.railRadius, "")
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
				d.k.Image(2, h, ui.FitFill, d.ui.railRadius, "")
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
		s := d.spaces()
		s.adding, d.spaces().err = true, ""
		s.newName.SetString("")
		s.invite.SetString("")
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
	d.ui.railRadius = d.px(radius) // the box clips square: the image rounds itself
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

// Guild header menu items.
const (
	guildMenuInvite = iota + 1
	guildMenuSettings
	guildMenuCreate
	guildMenuLeave
)

// guildHeader is GuildHeader: the name (over the banner when there is one) opening the guild's menu.
func (d *Discordo) guildHeader(g *Guild) {
	k, c := d.k, d.k.Context()
	key := ui.HashString(c.ParentKey(), "guild header")
	open := k.Popup().OpenBy(base.PopupItems, key)

	b := box{axis: ui.AxisX, w: ui.Pct(1), h: ui.Px(d.px(headerH)), l: 16, r: 16, gap: 8, cross: ui.CrossCenter}
	hover := uint32(surfaceHover)
	banner := g.BannerURL != ""
	if banner {
		b = box{w: ui.Pct(1), h: ui.Px(d.px(112)), clip: true}
		hover = 0
	}

	d.press(key, b, hover, g.Name)

	// Over a banner (h-28) the picture, a shade and the name at the bottom are all anchored: anchored boxes
	// draw over the flow, in the order they are built.
	if banner {
		w, h := d.px(sidebarW), d.px(112)
		if pic, st := d.image(g.BannerURL, int32(w), int32(h), true); st == img.Ready {
			c.NextAnchor(ui.AnchorTopLeft, 0, 0)
			c.NextSize(ui.Px(w), ui.Px(h))
			k.Image(5, pic, ui.FitFill, 0, "")
		}

		// The web's gradient to black/60 under the name: a shade over the lower half.
		c.NextAnchor(ui.AnchorBottomLeft, 0, 0)
		c.NextSize(ui.Px(w), ui.Px(h/2))
		c.NextBG(0x00000073)
		c.Box(ui.DrawBackground, "shade")

		c.NextAnchor(ui.AnchorBottomLeft, 0, 0)
		d.begin("title", box{axis: ui.AxisX, w: ui.Px(w), l: 16, r: 16, b: 12, gap: 8, cross: ui.CrossCenter})
	}

	d.text(g.Name, d.ui.s16b, snow)
	d.spacer()

	// The chevron turns up while the menu is open (the web rotates it).
	if open {
		d.icon("chevron-up", 16, fog)
	} else {
		d.icon("chevron-down", 16, fog)
	}

	if banner {
		c.Pop()
	}

	id := c.Parent()
	c.Pop()

	item, ok := rexui.DropdownMenu(k.Kit, id, func(items *[]ui.MenuItem) {
		*items = append(*items, ui.MenuItem{ID: guildMenuInvite, Title: "Convidar pessoas", Icon: "user-plus", Disabled: !has(g.Permissions, permInvite)})

		if len(settingsSections(g, d.st.Me.ID)) > 0 {
			*items = append(*items, ui.MenuItem{ID: guildMenuSettings, Title: "Configurações do servidor", Icon: "settings"})
		}

		if has(g.Permissions, permManage) {
			*items = append(*items, ui.MenuItem{ID: guildMenuCreate, Title: "Criar canal", Icon: "plus"})
		}

		if d.st.Me.ID != g.OwnerID {
			*items = append(*items, ui.MenuSeparator, ui.MenuItem{ID: guildMenuLeave, Title: "Sair do servidor", Icon: "log-out", Destructive: true})
		}
	})
	if !ok {
		return
	}

	s := d.spaces()
	d.spaces().err = ""

	switch item {
	case guildMenuInvite:
		s.inviting, s.link, s.inviteGuildID = true, "", g.ID
	case guildMenuSettings:
		d.openServerSettings(g)
	case guildMenuCreate:
		d.openCreateChannel()
	case guildMenuLeave:
		s.leaving = true
	}
}

func (d *Discordo) openCreateChannel() {
	s := d.spaces()
	s.creating, s.channelType, d.spaces().err = true, 0, ""
	s.channelName.SetString("")
}

// guildSidebar is features/spaces/guild-sidebar.tsx: the header with the guild's name, then the channels.
func (d *Discordo) guildSidebar(g *Guild, H float32) {
	k, c := d.k, d.k.Context()

	d.guildHeader(g)
	d.rule(line10, false)

	k.NextSize(ui.Pct(1), ui.Grow(1))
	v := k.BeginScroll("channels", &d.sideScroll, base.ScrollVertical)
	d.begin("list", box{axis: ui.AxisY, w: ui.Pct(1), l: 8, r: 8, t: 12, b: 12, gap: 16})

	sections := [...]struct {
		typ   int
		label string
		icon  string
	}{{ChannelText, "CANAIS DE TEXTO", "hash"}, {ChannelVoice, "CANAIS DE VOZ", "volume-2"}}

	canManage := has(g.Permissions, permManage)

	if i := slices.IndexFunc(g.Channels, func(ch Channel) bool { return ch.Type == ChannelWorld }); i >= 0 {
		d.begin("mundo", box{axis: ui.AxisY, w: ui.Pct(1), gap: 2})
		d.channelRow(g, &g.Channels[i], "globe")
		c.Pop()
	}

	for _, s := range sections {
		any := slices.ContainsFunc(g.Channels, func(ch Channel) bool { return ch.Type == s.typ })
		if !any && !canManage {
			continue
		}

		d.begin(s.label, box{axis: ui.AxisY, w: ui.Pct(1), gap: 2})

		d.begin("label", box{axis: ui.AxisX, w: ui.Pct(1), l: 6, r: 6, b: 2, cross: ui.CrossCenter})
		d.text(s.label, d.ui.caption, alpha(greyple, 0.8))
		d.spacer()

		if canManage && d.guildIconPress("plus", "Criar canal", 14, alpha(greyple, 0.8)) {
			d.openCreateChannel()
		}

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

			d.channelRow(g, ch, s.icon)
		}

		c.Pop()
	}

	c.Pop()
	k.CloseScrollContent(&v)
	rexui.ScrollBars(k.Kit, &v)
	k.CloseScroll(&v)

	d.userDock()
}

// channelRow is a channel in the sidebar: its icon and name, the gear for who manages it, the callers under it.
func (d *Discordo) channelRow(g *Guild, ch *Channel, iconName string) {
	c := d.k.Context()

	active := ch.ID == d.channel
	bg := uint32(0)
	if active {
		bg = surfaceActive
	}

	hot := d.press(ui.HashString(c.ParentKey(), ch.ID), box{axis: ui.AxisX, w: ui.Pct(1), l: 8, r: 6, t: 6, b: 6, gap: 8, radius: 6, bg: bg, cross: ui.CrossCenter}, surfaceHover, ch.Name)
	color := uint32(greyple)
	if active || hot {
		color = snow
	}

	d.icon(iconName, 18, color)
	d.text(ch.Name, d.ui.s14, color)
	d.spacer()

	// The gear shows on hover, for who may manage this channel.
	gear := hot && has(ch.Permissions, permManage) && d.guildIconPress("settings", "Configurações de "+ch.Name, 14, greyple)

	if d.pop() && !gear {
		d.open(g.ID, ch.ID)

		// Going to the world enters it; a call under way elsewhere is kept until "Entrar no mundo".
		if ch.Type == ChannelWorld && vc.channel == "" && has(ch.Permissions, permConnect) {
			d.voiceJoin(ch.ID, ch.Permissions)
		}
	}

	if gear {
		d.openChannelSettings(g, ch)
	}

	if ch.Type == ChannelVoice || ch.Type == ChannelWorld {
		d.voiceOccupants(g, ch.ID)
	}
}

// guildIconPress is a bare icon button that turns snow on hover (the section "+" and the channel gear).
func (d *Discordo) guildIconPress(name, label string, pt float32, idle uint32) bool {
	c := d.k.Context()
	hot := d.press(ui.HashString(c.ParentKey(), label), box{cross: ui.CrossCenter, justify: ui.JustifyCenter}, 0, label)
	color := idle
	if hot {
		color = snow
	}

	d.icon(name, pt, color)
	id := c.Parent()
	clicked := d.pop()
	rexui.Tooltip(d.k.Kit, id, label)

	return clicked
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
		d.begin("who", box{axis: ui.AxisY, w: ui.Grow(1)})
		d.text(name, d.ui.s12, greyple)
		if status := d.worldStatus(v.Zone); status != "" {
			d.text(status, d.ui.s11, alpha(greyple, 0.8))
		}
		c.Pop()

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

// spacesDialogs draws the servers' dialogs: add a server, invite, create channel, leave, server and channel settings.
func (d *Discordo) spacesDialogs() {
	s := d.spaces()

	// A guild just created or joined: open it as soon as the gateway has it.
	if s.pending != "" && d.st.Guild(s.pending) != nil {
		d.open(s.pending, "")
		s.pending = ""
	}

	d.addServerDialog()

	if g := d.st.Guild(d.guild); g != nil {
		d.inviteDialog(g)
		d.createChannelDialog(g)
		d.leaveDialog(g)
	}

	d.channelSettingsDialog()
	d.serverSettingsDialog()
}

// guildLabel is the web's section-label (callers write it upper-case, as its CSS does).
func (d *Discordo) guildLabel(s string) { d.text(s, d.ui.caption, greyple) }

// guildError is FieldError: the last failure, in red.
func (d *Discordo) guildError() {
	if d.spaces().err != "" {
		d.para(d.spaces().err, d.ui.s14, red)
	}
}

func guildButton(d *Discordo, label string, v rexui.ButtonVariant, disabled bool) bool {
	k := d.k
	k.BeginDisabled(disabled)
	clicked := rexui.Button(k.Kit, label, rexui.ButtonOpts{Variant: v}).Clicked
	k.EndDisabled()

	return clicked && !disabled
}

// addServerDialog is server-rail.tsx's AddServerDialog: create a server by name, or join one by invite.
func (d *Discordo) addServerDialog() {
	k, c := d.k, d.k.Context()
	s := d.spaces()

	dlg := d.dialog("add server", &s.adding, rexui.DialogOpts{Title: "Adicionar um servidor", Width: 448})
	if dlg.Visible {
		d.para("Crie o seu próprio espaço ou entre em um com um convite.", d.ui.s14, fog)

		done := func(d *Discordo, g Guild) {
			s.adding, s.pending = false, g.ID
		}

		d.begin("create", box{axis: ui.AxisY, w: ui.Pct(1), t: 8, gap: 8})
		d.guildLabel("CRIAR SERVIDOR")
		d.begin("row", box{axis: ui.AxisX, w: ui.Pct(1), gap: 8, cross: ui.CrossCenter})
		k.NextWidth(ui.Grow(1))
		f := k.TextField("##guild name", &s.newName, "Nome do servidor")
		name := strings.TrimSpace(s.newName.String())
		if (guildButton(d, "Criar", rexui.ButtonPrimary, name == "") || f.Submitted) && name != "" {
			guildCall(d, "POST", "/guilds", map[string]string{"name": name}, done)
		}
		c.Pop()
		c.Pop()

		d.begin("join", box{axis: ui.AxisY, w: ui.Pct(1), t: 8, gap: 8})
		d.rule(line10, false)
		d.begin("gap", box{t: 4})
		c.Pop()
		d.guildLabel("ENTRAR COM CONVITE")
		d.begin("row", box{axis: ui.AxisX, w: ui.Pct(1), gap: 8, cross: ui.CrossCenter})
		k.NextWidth(ui.Grow(1))
		f = k.TextField("##invite", &s.invite, "https://…/invite/ABC123 ou ABC123")
		typed := strings.TrimSpace(s.invite.String())
		if (guildButton(d, "Entrar", rexui.ButtonDefault, typed == "") || f.Submitted) && typed != "" {
			if code := parseInviteCode(typed); code == "" {
				d.spaces().err = "Convite inválido. Cole o código ou o link inteiro."
			} else {
				guildCall(d, "POST", "/invites/"+code, nil, done)
			}
		}
		c.Pop()
		c.Pop()

		d.guildError()
	}

	rexui.EndDialog(k.Kit, &dlg)
}

// inviteDialog is guild-dialogs.tsx's InviteDialog: a link with an expiry and a use limit.
func (d *Discordo) inviteDialog(g *Guild) {
	k, c := d.k, d.k.Context()
	s := d.spaces()
	if s.inviteGuildID != g.ID {
		s.inviting = false
	}

	dlg := d.dialog("invite", &s.inviting, rexui.DialogOpts{Title: "Convidar para " + g.Name, Width: 448})
	if dlg.Visible {
		d.para("Gere um link e envie para quem você quer chamar.", d.ui.s14, fog)

		d.begin("limits", box{axis: ui.AxisX, w: ui.Pct(1), gap: 12, t: 4})
		d.begin("expiry", box{axis: ui.AxisY, w: ui.Grow(1), gap: 8})
		d.guildLabel("EXPIRA EM")
		k.NextWidth(ui.Pct(1))
		k.Select("Expira em", &s.expiry, inviteExpiryNames)
		c.Pop()
		d.begin("uses", box{axis: ui.AxisY, w: ui.Grow(1), gap: 8})
		d.guildLabel("MÁXIMO DE USOS")
		k.NextWidth(ui.Pct(1))
		k.Select("Máximo de usos", &s.uses, inviteUseNames)
		c.Pop()
		c.Pop()

		if s.link != "" {
			d.guildBox("link", box{axis: ui.AxisX, w: ui.Pct(1), bg: surfaceRaised, radius: 8, l: 12, r: 8, t: 8, b: 8, gap: 8, cross: ui.CrossCenter}, line10)
			d.text(s.link, d.ui.s14, blurpleSoft)
			d.spacer()
			label := "Copiar"
			if s.copied == s.link {
				label = "Copiado"
			}

			if guildButton(d, label+"###copy", rexui.ButtonPrimary, false) {
				d.guildCopy(s.link)
			}
			c.Pop()
		}

		d.guildError()

		label, v := "Gerar link", rexui.ButtonPrimary
		if s.link != "" {
			label, v = "Gerar outro link", rexui.ButtonDefault
		}

		d.begin("generate", box{axis: ui.AxisX, w: ui.Pct(1), t: 4})
		k.NextWidth(ui.Pct(1))
		if guildButton(d, label+"###generate", v, false) {
			body := map[string]int{"max_uses": inviteUses[s.uses], "max_age": inviteExpiry[s.expiry].seconds}
			guildCall(d, "POST", "/guilds/"+g.ID+"/invites", body, func(d *Discordo, inv Invite) {
				s.link = d.inviteLink(inv.Code)
			})
		}
		c.Pop()
	}

	rexui.EndDialog(k.Kit, &dlg)
}

// createChannelDialog is guild-dialogs.tsx's CreateChannelDialog: text or voice, and a name.
func (d *Discordo) createChannelDialog(g *Guild) {
	k, c := d.k, d.k.Context()
	s := d.spaces()

	dlg := d.dialog("create channel", &s.creating, rexui.DialogOpts{Title: "Criar canal", Width: 448})
	if dlg.Visible {
		d.para("Canais de texto para conversar, de voz para chamadas.", d.ui.s14, fog)

		// The radio cards: bordered tiles, the chosen one outlined in blurple-soft.
		d.begin("types", box{axis: ui.AxisX, w: ui.Pct(1), gap: 8, t: 4})
		for i, t := range [...]struct{ label, icon string }{{"Texto", "hash"}, {"Voz", "volume-2"}} {
			on := s.channelType == i
			border := line10
			if on {
				border = blurpleSoft
			}

			hot := d.guildPress(ui.HashString(c.ParentKey(), t.label), box{axis: ui.AxisX, w: ui.Grow(1), l: 12, r: 12, t: 10, b: 10, gap: 8, radius: 8, bg: surfaceRaised, cross: ui.CrossCenter}, 0, border, t.label)
			color := uint32(greyple)
			if on || hot {
				color = snow
			}

			d.icon(t.icon, 16, color)
			d.text(t.label, d.ui.s14, color)
			if d.pop() {
				s.channelType = i
			}
		}
		c.Pop()

		d.begin("name", box{axis: ui.AxisY, w: ui.Pct(1), gap: 8, t: 8})
		d.guildLabel("NOME DO CANAL")
		f := k.TextField("##channel name", &s.channelName, "novo-canal")
		c.Pop()

		d.guildError()

		name := strings.TrimSpace(s.channelName.String())
		d.begin("actions", box{axis: ui.AxisX, w: ui.Pct(1), gap: 8, t: 8})
		d.spacer()
		if guildButton(d, "Cancelar", rexui.ButtonDefault, false) {
			s.creating = false
		}

		if (guildButton(d, "Criar canal", rexui.ButtonPrimary, name == "") || f.Submitted) && name != "" {
			body := map[string]any{"name": name, "type": []int{ChannelText, ChannelVoice}[s.channelType]}
			guildCall(d, "POST", "/guilds/"+g.ID+"/channels", body, func(d *Discordo, _ Channel) { s.creating = false })
		}
		c.Pop()
	}

	rexui.EndDialog(k.Kit, &dlg)
}

// leaveDialog is the ConfirmDialog of leaving; the GUILD_DELETE that follows takes the workspace home.
func (d *Discordo) leaveDialog(g *Guild) {
	k := d.k
	s := d.spaces()

	dlg := d.dialog("leave", &s.leaving, rexui.DialogOpts{Title: "Sair de " + g.Name + "?"})
	if dlg.Visible {
		d.para("Você só volta com um novo convite.", d.ui.s14, fog)
		dlg.BeginActions()
		dlg.Action("Cancelar", rexui.ButtonDefault)

		if dlg.Action("Sair do servidor", rexui.ButtonDestructive) {
			guildCall(d, "DELETE", "/guilds/"+g.ID+"/members/@me", nil, func(*Discordo, struct{}) {})
		}

		dlg.EndActions()
	}

	rexui.EndDialog(k.Kit, &dlg)
}

// guildBox is begin with a 1 px border (the web's border-white/10): the shared box draws none.
func (d *Discordo) guildBox(label string, b box, border uint32) ui.ID {
	c := d.k.Context()
	flags := d.style(b) | ui.DrawBorder
	c.NextBorder(border, 1)
	id := c.Box(flags, label)
	c.Push(id)

	return id
}

// guildPress is press with a 1 px border (0: none). Close with pop.
func (d *Discordo) guildPress(key ui.Key, b box, hoverBG, border uint32, name string) (hot bool) {
	k, c := d.k, d.k.Context()
	hot = k.VisualOf(key, base.HoverOn).Hot > 0.5

	if hot && hoverBG != 0 {
		b.bg = hoverBG
	}

	flags := d.style(b) | k.InteractFlags(base.HoverOn)
	if border != 0 {
		flags |= ui.DrawBorder
		c.NextBorder(border, 1)
	}

	c.NextRole(ui.RoleButton)
	c.NextSemName(name)
	c.NextCursor(ui.CursorPointer)
	c.Push(c.BoxKeyed(flags, key, nil))

	return hot
}

// guildCall is call (kit.go) with the failure kept for the open dialog instead of the status line.
func guildCall[T any](d *Discordo, method, path string, body any, done func(*Discordo, T)) {
	s := d.spaces()
	client, gen := d.client, d.gen
	s.err = ""

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
				s.err = err.Error()
				return
			}

			done(d, out)
		})
	}()
}
