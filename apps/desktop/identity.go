package main

// Who I am and who they are: the user dock, profile cards, user settings.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"gonativeui/platform"
	"gonativeui/rexui"
	"gonativeui/theme"
	"gonativeui/ui"
	"gonativeui/widget/base"
)

// Limits of profile-card.tsx and user-settings-dialog.tsx.
const (
	maxDisplayName = 32
	maxPronouns    = 40
	maxBio         = 190
)

// bannerSwatches are user-settings-dialog.tsx's BANNER_SWATCHES.
var bannerSwatches = [...]int{0x5865f2, 0xeb459e, 0xf23f42, 0xf0b232, 0x23a55a, 0x00a8fc, 0x9b59b6, 0x2b2d31}

const (
	userProfileSection = iota
	userAccountSection
	userAppearanceSection
	userVoiceSection
)

// userSections are user-settings-dialog.tsx's SECTIONS with their descriptions.
var userSections = [...]struct{ icon, label, desc string }{
	{"user-round", "Perfil", "Como as pessoas veem você no Discordo."},
	{"id-card", "Minha conta", "Sua conta e a sessão neste aparelho."},
	{"palette", "Aparência", "Tema, modo de cor e efeitos."},
	{"volume-2", "Voz e vídeo", "Dispositivos e processamento do microfone."},
}

// profileValues are the profile form's fields (ProfileInput), comparable to tell a dirty form.
type profileValues struct {
	name, avatar, pronouns, bio string
	banner                      int
}

// idn is identity's state: the shared Discordo struct is frozen, and there is one window.
var idn struct {
	profileOpen bool
	profileID   string
	profiles    map[string]Profile // fetched cards, by user

	settingsOpen bool
	section      int
	scroll       base.ScrollState

	formReady           bool
	base                profileValues // what the server has: the form is dirty when it differs
	avatar              string
	banner              uint32 // 0xRRGGBBAA for the colour well; 0 is the default banner
	name, pronouns, bio ui.TextBuffer
	well                rexui.ColorWellState
	saving, saved       bool
	uploading, picking  bool
	ticket              platform.DialogTicket
	copied              bool
}

// userDock is features/shell/user-dock.tsx with features/identity/user-panel.tsx.
func (d *Discordo) userDock() {
	c := d.k.Context()
	c0 := d.k.Context()
	c0.PushOpacity(d.fadeIn("call panel " + vc.channel)) // it fades in as the call starts
	d.voicePanel()                                       // the call I am in, above the dock (voice-dock.tsx)
	c0.PopOpacity()
	d.rule(line10, false)
	d.begin("dock", box{axis: ui.AxisX, w: ui.Pct(1), h: ui.Px(d.px(56)), l: 8, r: 8, gap: 8, bg: surfaceRail, cross: ui.CrossCenter})

	// The avatar and the names are one button: my profile card.
	me := d.st.Me
	d.press(ui.HashString(c.ParentKey(), "me"), box{axis: ui.AxisX, w: ui.Grow(1), gap: 8, radius: 6, cross: ui.CrossCenter}, 0, "Meu perfil")
	d.avatar(me, me.Name(), 32, rexui.PresenceOnline, surfaceRail)

	d.begin("who", box{axis: ui.AxisY, w: ui.Grow(1)})
	d.text(me.Name(), d.ui.s12s, snow)
	d.text("@"+me.Username, d.ui.s11, greyple)
	c.Pop()

	if d.pop() {
		d.openProfile(me.ID)
	}

	if d.ghost("settings", "Configurações", 16) {
		d.openSettings()
	}

	if d.ghost("log-out", "Sair", 16) {
		d.signOut("")
	}

	c.Pop()
}

// openProfile shows someone's profile card (profile-viewer.tsx), fetching its details.
func (d *Discordo) openProfile(userID string) {
	idn.profileOpen, idn.profileID = true, userID
	d.status = ""
	d.fetchProfile(userID)
}

// openSettings opens the user settings at Perfil (user-settings-dialog.tsx).
func (d *Discordo) openSettings() {
	idn.settingsOpen, idn.section = true, userProfileSection
	idn.formReady, idn.saved, idn.copied = false, false, false
	d.status = ""
	d.fetchProfile(d.st.Me.ID)
}

func (d *Discordo) fetchProfile(userID string) {
	call(d, "GET", "/users/"+userID+"/profile", nil, func(d *Discordo, p Profile) {
		if idn.profiles == nil {
			idn.profiles = map[string]Profile{}
		}

		idn.profiles[userID] = p
	})
}

// identityDialogs draws the profile card and the user settings.
func (d *Discordo) identityDialogs() {
	if d.phase != phaseOnline || !d.ready {
		idn.profileOpen, idn.settingsOpen = false, false
		return
	}

	// The open panel answers between frames.
	if idn.picking && d.a != nil {
		if r, ok := d.a.TakeDialogFor(idn.ticket); ok {
			idn.picking = false

			if len(r.Paths) > 0 {
				d.uploadAvatar(r.Paths[0])
			}
		}
	}

	d.profileDialog()
	d.settingsDialog()
}

// idDialog is a dialog sheet without padding, filled with fill, for content that draws its own (a card,
// the settings). DialogLook is read only in BeginDialog, so it is restored at once.
func (d *Discordo) idDialog(label string, open *bool, width float32, fill uint32) rexui.Dialog {
	saved := rexui.DialogLook
	l := &rexui.DialogLook
	l.PadX, l.PadTop, l.PadBottom = 0, 0, 0
	l.Fill, l.Blur.Tint, l.Blur.Mix = rexui.OfRGBA(fill), rexui.OfRGBA(fill), [2]float32{1, 1}

	dlg := d.dialog(label, open, rexui.DialogOpts{Width: width})
	rexui.DialogLook = saved

	return dlg
}

// cardFill is bg-popover, the card's fill: the rail's colour, so a theme changes it too.
func cardFill() uint32 { return surfaceRail }

// profileDialog is profile-viewer.tsx's ProfileCard in its dialog (max-w-sm).
func (d *Discordo) profileDialog() {
	dlg := d.idDialog("profile", &idn.profileOpen, 384, cardFill())

	if dlg.Visible {
		id := idn.profileID
		p := idn.profiles[id] // the details, while they last; the live user wins below
		p.ID = id

		// The live user wins over the fetched one: a USER_UPDATE renames the card at once.
		self := id == d.st.Me.ID
		if self {
			p.User = d.st.Me
		} else if u, ok := d.st.User(id); ok {
			p.User = u
		}

		if p.Username == "" {
			d.begin("missing", box{w: ui.Pct(1), l: 24, r: 24, t: 24, b: 24})
			d.text("Usuário indisponível.", d.ui.s14, fog)
			d.k.Context().Pop()
		} else {
			corner := func() {
				if d.idButton("pencil", "Editar perfil", idBtnSecondary, false) {
					idn.profileOpen = false
					d.openSettings()
				}
			}

			if !self {
				corner = nil
			}

			var actions func()
			if !self {
				actions = func() { d.profileActions(p.User) }
			}

			d.profileCard(p, self || d.st.Online(id), corner, actions)
		}
	}

	rexui.EndDialog(d.k.Kit, &dlg)
}

// profileActions are the card's Mensagem and FriendAction.
func (d *Discordo) profileActions(u User) {
	if d.idButton("message-circle", "Mensagem", idBtnPrimary, false) {
		call(d, "POST", "/dms", map[string]string{"recipient_id": u.ID}, func(d *Discordo, dm DM) {
			idn.profileOpen = false
			d.open("", dm.ID)
		})
	}

	status := 0
	for _, r := range d.st.Relationships {
		if r.User.ID == u.ID {
			status = r.Status
		}
	}

	switch status {
	case 1:
		d.outlineBadge("check", "Amigos")
	case 3:
		d.outlineBadge("", "Pedido enviado")
	case 2:
		if d.idButton("user-plus", "Aceitar amizade", idBtnSecondary, false) {
			call(d, "PUT", "/relationships/"+u.ID, nil, func(*Discordo, struct{}) {})
		}
	case 0:
		if d.idButton("user-plus", "Adicionar amigo", idBtnSecondary, false) {
			call(d, "POST", "/relationships", map[string]string{"username": u.Username}, func(*Discordo, struct{}) {})
		}
	}

	if d.status != "" {
		d.text(d.status, d.ui.s12, red)
	}
}

// profileCard is profile-card.tsx's ProfileCardView: the banner, the avatar, the names, about me, member
// since, the servers in common; corner goes next to the avatar and actions under everything.
func (d *Discordo) profileCard(p Profile, online bool, corner, actions func()) {
	c := d.k.Context()
	bg := cardFill()
	d.begin("card", box{axis: ui.AxisY, w: ui.Pct(1), bg: bg, radius: 12, clip: true})

	c.NextSize(ui.Pct(1), ui.Px(d.px(96)))
	tl, br := bannerFill(p.BannerColor, bg)
	c.NextBG4(tl, theme.MixRGBA(tl, br, 0.5), theme.MixRGBA(tl, br, 0.5), br)
	c.BoxKeyed(ui.DrawBackground, ui.HashString(c.ParentKey(), "banner"), nil)

	d.begin("body", box{axis: ui.AxisY, w: ui.Pct(1), l: 20, r: 20, b: 20})

	// -mt-10: the avatar (80 in a ring of 4) rises 40 into the banner.
	d.begin("head", box{axis: ui.AxisX, w: ui.Pct(1), h: ui.Px(d.px(48)), cross: ui.CrossEnd})
	theme.At(c, 0, -d.px(40), d.px(88), d.px(88))
	d.begin("ring", box{floating: true, radius: 44, bg: bg, cross: ui.CrossCenter, justify: ui.JustifyCenter})
	presence := rexui.PresenceNone
	if online {
		presence = rexui.PresenceOnline
	}

	d.avatar(p.User, p.Name(), 80, presence, bg)
	c.Pop()

	d.spacer()
	if corner != nil {
		corner()
	}
	c.Pop()

	d.begin("gap", box{t: 12})
	c.Pop()

	d.para(p.Name(), d.ui.s18b, snow)
	handle := "@" + p.Username
	if p.Pronouns != "" {
		handle += " · " + p.Pronouns
	}

	d.text(handle, d.ui.s14m, greyple)

	if p.Bio != "" || !p.CreatedAt.IsZero() || len(p.MutualGuildIDs) > 0 {
		d.begin("blocks", box{axis: ui.AxisY, w: ui.Pct(1), t: 12, gap: 16})
		d.begin("pad", box{axis: ui.AxisY, w: ui.Pct(1), t: 4, gap: 16})

		if p.Bio != "" {
			d.profileBlock("Sobre mim", func() { d.para(p.Bio, d.ui.s14, fog) })
		}

		if !p.CreatedAt.IsZero() {
			d.profileBlock("Membro do Discordo desde", func() { d.text(memberSince(p.CreatedAt), d.ui.s14, fog) })
		}

		if n := len(p.MutualGuildIDs); n > 0 {
			title := itoa(n) + " servidores em comum"
			if n == 1 {
				title = "Servidor em comum"
			}

			// Only the servers this client knows, by name.
			var names []string
			for _, g := range d.st.Guilds {
				for _, id := range p.MutualGuildIDs {
					if g.ID == id {
						names = append(names, g.Name)
					}
				}
			}

			d.profileBlock(title, func() {
				if len(names) > 0 {
					d.para(strings.Join(names, ", "), d.ui.s14, fog)
				}
			})
		}

		c.Pop()
		c.Pop()
	}

	if actions != nil {
		d.begin("actions", box{axis: ui.AxisY, w: ui.Pct(1), t: 16, gap: 16})
		d.rule(line10, false)
		d.begin("row", box{axis: ui.AxisX, w: ui.Pct(1), gap: 8, cross: ui.CrossCenter})
		actions()
		c.Pop()
		c.Pop()
	}

	c.Pop()
	c.Pop()
}

// profileBlock is profile-card.tsx's Block: an uppercase title over its content.
func (d *Discordo) profileBlock(title string, content func()) {
	d.begin("block", box{axis: ui.AxisY, w: ui.Pct(1), gap: 4})
	d.text(strings.ToUpper(title), d.ui.caption, snow)
	content()
	d.k.Context().Pop()
}

// bannerFill is the banner's top-left and bottom-right colours: the profile's colour, or the theme's
// gradient (from-blurple/60 to-fuchsia/40) over the card.
func bannerFill(color int, bg uint32) (tl, br uint32) {
	if color != 0 {
		c := uint32(color)<<8 | 0xff
		return c, c
	}

	return theme.Over(alpha(blurple, 0.6), bg), theme.Over(alpha(fuchsia, 0.4), bg)
}

var ptMonthsShort = [...]string{"jan.", "fev.", "mar.", "abr.", "mai.", "jun.", "jul.", "ago.", "set.", "out.", "nov.", "dez."}

// memberSince is profile-card.tsx's: toLocaleDateString('pt-BR', {day, month: 'short', year}).
func memberSince(t time.Time) string {
	t = t.Local()
	return itoa(t.Day()) + " de " + ptMonthsShort[t.Month()-1] + " de " + itoa(t.Year())
}

// clampRunes cuts a field to n characters: maxLength, counted as the web counts the form.
func clampRunes(b *ui.TextBuffer, n int) {
	s := b.String()
	if utf8.RuneCountInString(s) <= n {
		return
	}

	i, k := 0, 0
	for i = range s {
		if k == n {
			break
		}
		k++
	}

	b.SetString(s[:i])
}

const (
	idBtnPrimary = iota
	idBtnSecondary
	idBtnGhost
	idBtnDestructive
)

// button is the web's sm Button with an optional icon: primary, secondary, ghost or destructive.
func (d *Discordo) idButton(iconName, label string, kind int, disabled bool) bool {
	c := d.k.Context()
	bg, hover, fg := uint32(blurple), uint32(0x4752c4ff), uint32(white)

	switch kind {
	case idBtnSecondary:
		bg, hover, fg = surfaceRaised, surfaceActive, snow
	case idBtnGhost:
		bg, hover, fg = 0, surfaceHover, fog
	case idBtnDestructive:
		bg, hover = red, 0xda373cff
	}

	if disabled {
		hover = 0
		fg = alpha(fg, 0.5)
		if bg != 0 {
			bg = alpha(bg, 0.5)
		}
	}

	hot := d.press(ui.HashString(c.ParentKey(), label), box{axis: ui.AxisX, h: ui.Px(d.px(32)), l: 12, r: 12, gap: 6, radius: 8, bg: bg, cross: ui.CrossCenter}, hover, label)
	if hot && kind == idBtnGhost && !disabled {
		fg = snow
	}

	if iconName != "" {
		d.icon(iconName, 16, fg)
	}

	d.text(label, d.ui.s14m, fg)

	return d.pop() && !disabled
}

// outlineBadge is the web's outline Badge: Amigos, Pedido enviado.
func (d *Discordo) outlineBadge(iconName, label string) {
	c := d.k.Context()
	outline(c, line10, 1)
	d.begin("badge "+label, box{axis: ui.AxisX, h: ui.Px(d.px(22)), l: 8, r: 8, gap: 4, radius: 6, cross: ui.CrossCenter})

	if iconName != "" {
		d.icon(iconName, 12, snow)
	}

	d.text(label, d.ui.s12s, snow)
	c.Pop()
}

// settingsDialog is user-settings-dialog.tsx: the sections at the left, the open one at the right.
func (d *Discordo) settingsDialog() {
	k, c := d.k, d.k.Context()
	w, h := c.Size()
	sc := c.Scale()
	W, H := float32(w)/sc, float32(h)/sc

	dlg := d.idDialog("settings", &idn.settingsOpen, min(W-32, 1024), surfaceContent)
	if !dlg.Visible {
		rexui.EndDialog(k.Kit, &dlg)
		return
	}

	// h-[min(88vh,52rem)]
	d.begin("settings", box{axis: ui.AxisX, w: ui.Pct(1), h: ui.Px(d.px(min(0.88*H-48, 832)))})

	rs := k.Theme().Radii.Sheet
	c.NextRadii(rs, 0, 0, rs)
	d.begin("nav", box{axis: ui.AxisY, w: ui.Px(d.px(224)), h: ui.Pct(1), l: 16, r: 16, t: 16, b: 16, gap: 4, bg: surfaceSidebar})
	d.begin("label", box{l: 8, b: 4})
	d.text("CONFIGURAÇÕES", d.ui.caption, greyple)
	c.Pop()

	for i, s := range userSections {
		active := idn.section == i
		bg := uint32(0)
		if active {
			bg = surfaceActive
		}

		hot := d.press(ui.HashString(c.ParentKey(), s.label), box{axis: ui.AxisX, w: ui.Pct(1), l: 10, r: 10, t: 8, b: 8, gap: 8, radius: 6, bg: bg, cross: ui.CrossCenter}, surfaceHover, s.label)
		color := uint32(fog)
		if active || hot {
			color = snow
		}

		d.icon(s.icon, 16, color)
		d.text(s.label, d.ui.s14m, color)

		if d.pop() {
			idn.section = i
			d.status = ""
		}
	}
	c.Pop()

	k.NextSize(ui.Grow(1), ui.Pct(1))
	v := k.BeginScroll("section", &idn.scroll, base.ScrollVertical)
	d.begin("content", box{axis: ui.AxisY, w: ui.Pct(1), l: 32, r: 32, t: 32, b: 32})

	s := userSections[idn.section]
	d.begin("title", box{axis: ui.AxisX, w: ui.Pct(1), cross: ui.CrossCenter})
	d.text(s.label, d.ui.s20b, snow)
	d.spacer()
	if d.ghost("x", "Fechar", 16) {
		idn.settingsOpen = false
	}
	c.Pop()

	d.begin("desc", box{t: 4, b: 24})
	d.text(s.desc, d.ui.s14, fog)
	c.Pop()

	switch idn.section {
	case userProfileSection:
		d.profileSection(W >= 1024)
	case userAccountSection:
		d.accountSection()
	case userAppearanceSection:
		d.appearanceSection()
	case userVoiceSection:
		d.voiceSettingsSection()
	}

	c.Pop()
	k.CloseScrollContent(&v)
	rexui.ScrollBars(k.Kit, &v)
	k.CloseScroll(&v)
	c.Pop()

	rexui.EndDialog(k.Kit, &dlg)
}

// profileFormValues is the form as typed.
func profileFormValues() profileValues {
	return profileValues{idn.name.String(), idn.avatar, idn.pronouns.String(), idn.bio.String(), int(idn.banner >> 8)}
}

// resetProfileForm puts v in the form (useForm's reset).
func resetProfileForm(v profileValues) {
	idn.base, idn.avatar = v, v.avatar
	idn.name.SetString(v.name)
	idn.pronouns.SetString(v.pronouns)
	setArea(bioArea, &idn.bio, v.bio)
	idn.banner = 0
	if v.banner != 0 {
		idn.banner = uint32(v.banner)<<8 | 0xff
	}
}

// profileSection is ProfileSection and ProfileForm: the fields, then the live preview of the card.
func (d *Discordo) profileSection(wide bool) {
	k, c := d.k, d.k.Context()
	me := d.st.Me
	p, ok := idn.profiles[me.ID]

	if !ok {
		if d.status != "" {
			d.text(d.status, d.ui.s14, red)
		} else {
			rexui.Spinner(k.Kit, "Carregando o perfil", rexui.SpinnerOpts{})
		}

		return
	}

	if !idn.formReady {
		resetProfileForm(profileValues{me.DisplayName, me.AvatarURL, p.Pronouns, p.Bio, p.BannerColor})
		idn.formReady = true
	}

	axis := ui.AxisY
	if wide {
		axis = ui.AxisX
	}

	d.begin("profile", box{axis: axis, w: ui.Pct(1), gap: 32})
	fieldsW := ui.Pct(1)
	if wide {
		fieldsW = ui.Grow(1)
	}

	d.begin("fields", box{axis: ui.AxisY, w: fieldsW, gap: 20})

	d.idField("Avatar", func() {
		d.begin("avatar", box{axis: ui.AxisX, gap: 8, cross: ui.CrossCenter})
		label := "Trocar avatar"
		if idn.uploading {
			rexui.Spinner(k.Kit, "Enviando", rexui.SpinnerOpts{})
		}

		if d.idButton("image-up", label, idBtnSecondary, idn.uploading || idn.picking) {
			d.pickAvatar()
		}

		if idn.avatar != "" && d.idButton("trash-2", "Remover", idBtnGhost, false) {
			idn.avatar = ""
		}
		c.Pop()
	})

	d.idField("Nome de exibição", func() {
		k.TextField("##profile-name", &idn.name, "")
		clampRunes(&idn.name, maxDisplayName)
	})

	d.idField("Pronomes", func() {
		k.TextField("##profile-pronouns", &idn.pronouns, "ele/dele, ela/dela, elu/delu…")
		clampRunes(&idn.pronouns, maxPronouns)
	})

	d.idField("Sobre mim", func() {
		d.areaField("##profile-bio", bioArea, &idn.bio)
		if clampRunes(&idn.bio, maxBio); bioArea != nil && string(bioArea.Text()) != idn.bio.String() {
			bioArea.SetText([]byte(idn.bio.String()))
		}
		d.begin("count", box{axis: ui.AxisX, w: ui.Pct(1)})
		d.spacer()
		d.text(itoa(utf8.RuneCountInString(idn.bio.String()))+"/"+itoa(maxBio), d.ui.s12, greyple)
		c.Pop()
	})

	d.idField("Cor do banner", d.bannerPicker)

	cur := profileFormValues()
	dirty := cur != idn.base

	if d.status != "" {
		d.para(d.status, d.ui.s14, red)
	}

	d.begin("save", box{axis: ui.AxisX, gap: 8, cross: ui.CrossCenter})
	label, iconName := "Salvar alterações", ""
	if idn.saved && !dirty {
		label, iconName = "Salvo", "check"
	}

	if idn.saving {
		rexui.Spinner(k.Kit, "Salvando", rexui.SpinnerOpts{})
	}

	if d.idButton(iconName, label, idBtnPrimary, !dirty || idn.uploading || idn.saving || strings.TrimSpace(cur.name) == "") {
		d.saveProfile(cur)
	}

	if dirty && d.idButton("", "Desfazer", idBtnGhost, false) {
		resetProfileForm(idn.base)
	}
	c.Pop()

	c.Pop()

	previewW := ui.Pct(1)
	if wide {
		previewW = ui.Px(d.px(320))
	}

	d.begin("preview", box{axis: ui.AxisY, w: previewW, gap: 8})
	d.text("PRÉVIA", d.ui.caption, greyple)

	pv := p
	pv.User = me
	pv.DisplayName, pv.AvatarURL, pv.Pronouns, pv.Bio, pv.BannerColor = cur.name, cur.avatar, cur.pronouns, cur.bio, cur.banner
	if pv.DisplayName == "" {
		pv.DisplayName = me.Username
	}

	pv.MutualGuildIDs = nil
	d.profileCard(pv, true, nil, nil)
	c.Pop()

	c.Pop()
}

// field is a Field with its section-label.
func (d *Discordo) idField(label string, content func()) {
	d.begin("field "+label, box{axis: ui.AxisY, w: ui.Pct(1), gap: 8})
	d.text(strings.ToUpper(label), d.ui.caption, greyple)
	content()
	d.k.Context().Pop()
}

// bannerPicker is the "Cor do banner" radio group: Padrão (the gradient), the swatches, another colour.
func (d *Discordo) bannerPicker() {
	c := d.k.Context()
	// Two rows: flex-wrap's look at the form's width (boxes do not wrap).
	d.begin("banner", box{axis: ui.AxisY, gap: 8})
	d.begin("row 1", box{axis: ui.AxisX, gap: 8, cross: ui.CrossCenter})
	current := int(idn.banner >> 8)

	ring := func(on bool) {
		if on {
			outline(c, snow, d.px(2))
		}
	}

	// The gradient is a box inside the button: press draws one colour.
	ring(current == 0)
	d.press(ui.HashString(c.ParentKey(), "default"), box{h: ui.Px(d.px(32)), radius: 6, clip: true}, 0, "Padrão")
	tl, br := bannerFill(0, surfaceContent)
	flags := d.style(box{axis: ui.AxisX, h: ui.Pct(1), l: 12, r: 12, cross: ui.CrossCenter})
	c.NextBG4(tl, theme.MixRGBA(tl, br, 0.5), theme.MixRGBA(tl, br, 0.5), br)
	c.Push(c.Box(flags|ui.DrawBackground, "gradient"))
	d.text("Padrão", d.ui.s12s, white)
	c.Pop()
	if d.pop() {
		idn.banner = 0
	}

	for i, color := range bannerSwatches {
		if i == 4 {
			c.Pop()
			d.begin("row 2", box{axis: ui.AxisX, gap: 8, cross: ui.CrossCenter})
		}

		rgba := uint32(color)<<8 | 0xff
		ring(current == color)
		d.press(ui.HashString(c.ParentKey(), itoa(color)), box{w: ui.Px(d.px(32)), h: ui.Px(d.px(32)), radius: 6, bg: rgba}, 0, "#"+idHex6(color))
		if d.pop() {
			idn.banner = rgba
		}
	}

	well := idn.banner
	if well == 0 {
		well = 0x5865f2ff
	}

	if rexui.ColorWell(d.k.Kit, "Outra cor", &idn.well, &well, rexui.ColorWellOpts{}).Changed {
		idn.banner = well | 0xff
		if idn.banner>>8 == 0 {
			idn.banner = 0x000001ff // pure black would read as 0, the default banner
		}
	}

	c.Pop()
	c.Pop()
}

func idHex6(n int) string {
	const digits = "0123456789abcdef"
	var b [6]byte
	for i := 5; i >= 0; i-- {
		b[i] = digits[n&0xf]
		n >>= 4
	}

	return string(b[:])
}

// pickAvatar asks for a picture with the system's open panel; identityDialogs takes the answer.
func (d *Discordo) pickAvatar() {
	if d.a == nil {
		return
	}

	t, err := d.a.OpenPanel(platform.OpenSpec{Types: []string{".png", ".jpg", ".jpeg", ".gif", ".webp"}, Prompt: "Trocar avatar"})
	if err != nil {
		d.status = err.Error()
		return
	}

	idn.ticket, idn.picking = t, true
}

// uploadAvatar sends the picture to /images; its URL becomes the form's avatar.
func (d *Discordo) uploadAvatar(path string) {
	client, gen := d.client, d.gen
	idn.uploading = true
	d.status = ""

	go func() {
		var out struct {
			URL string `json:"url"`
		}

		data, err := os.ReadFile(path)
		if err == nil {
			err = client.Upload(context.Background(), "/images", filepath.Base(path), data, &out)
		}

		d.post(func(d *Discordo) {
			idn.uploading = false
			if d.gen != gen {
				return
			}

			if err != nil {
				d.status = err.Error()
				return
			}

			idn.avatar = out.URL
		})
	}()
}

// saveProfile is PATCH /users/@me; USER_UPDATE refreshes Me, and the card is fetched again.
func (d *Discordo) saveProfile(v profileValues) {
	idn.saving = true
	body := map[string]any{"display_name": v.name, "avatar_url": v.avatar, "pronouns": v.pronouns, "bio": v.bio, "banner_color": v.banner}

	callThen(d, "PATCH", "/users/@me", body, func(d *Discordo, u User) {
		idn.saved, idn.base = true, v
		d.fetchProfile(u.ID)
	}, func(*Discordo) { idn.saving = false })
}

// callThen is call with always run after the answer, failed or not: to end a pending state.
func callThen[T any](d *Discordo, method, path string, body any, done func(*Discordo, T), always func(*Discordo)) {
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
			always(d)
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

// accountSection is AccountSection: the username, since when, the ID to copy, and signing out.
func (d *Discordo) accountSection() {
	c := d.k.Context()
	me := d.st.Me

	d.begin("account", box{axis: ui.AxisY, w: ui.Pct(1), gap: 24})
	outline(c, line10, 1)
	d.begin("rows", box{axis: ui.AxisY, w: ui.Pct(1), l: 16, r: 16, radius: 12, bg: surfaceRaised})

	since := "…"
	if p, ok := idn.profiles[me.ID]; ok {
		since = memberSince(p.CreatedAt)
	}

	row := func(label, value string, copy bool) {
		d.begin("row "+label, box{axis: ui.AxisX, w: ui.Pct(1), t: 12, b: 12, gap: 16, cross: ui.CrossCenter})
		d.begin("text", box{axis: ui.AxisY, w: ui.Grow(1), gap: 2})
		d.text(strings.ToUpper(label), d.ui.caption, greyple)
		d.text(value, d.ui.s14, snow)
		c.Pop()

		if copy {
			label, iconName := "Copiar", "copy"
			if idn.copied {
				label, iconName = "Copiado", "check"
			}

			if d.idButton(iconName, label, idBtnSecondary, false) {
				platform.SetClipboardText([]byte(value))
				idn.copied = true
			}
		}
		c.Pop()
	}

	row("Usuário", "@"+me.Username, false)
	d.rule(line10, false)
	row("Membro desde", since, false)
	d.rule(line10, false)
	row("ID do usuário", me.ID, true)
	c.Pop()

	d.para("O login é pela sua conta do Discord. O nome de usuário vem de lá; nome de exibição e avatar trocados aqui continuam valendo nos próximos logins.", d.ui.s12, greyple)

	d.begin("out", box{axis: ui.AxisX})
	if d.idButton("log-out", "Sair do Discordo", idBtnDestructive, false) {
		idn.settingsOpen = false
		d.signOut("")
	}
	c.Pop()

	c.Pop()
}
