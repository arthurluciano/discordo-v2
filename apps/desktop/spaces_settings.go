package main

// Server settings (server-settings-dialog.tsx and settings/*.tsx): overview (name, icon, banner), roles,
// members, invites, bans, and deleting the server.

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gonativeui/img"
	"gonativeui/platform"
	"gonativeui/rexui"
	"gonativeui/ui"
	"gonativeui/widget/base"
)

type serverSettings struct {
	open    bool
	guildID string
	tab     int

	// Visão geral
	name               ui.TextBuffer
	icon, banner       string
	picking, uploading string // "icon" or "banner": the open panel's, the upload's
	ticket             platform.DialogTicket

	// Cargos
	role, roleFor string // the selected role, and the role the form below was filled from
	roleName      ui.TextBuffer
	roleColor     uint32 // 0xRRGGBBAA; 0 is no colour
	well          rexui.ColorWellState
	roleConfirm   bool

	// Membros
	search    ui.TextBuffer
	punishing string // the member whose kick or ban is being confirmed
	punishBan bool

	// Convites, Banimentos: fetched while their tab is open.
	fetched string
	loaded  bool
	invites []Invite
	bans    []Ban

	// Excluir servidor
	typed ui.TextBuffer

	scroll base.ScrollState
}

type guildSection struct{ key, label string }

// settingsSections is the web's: the tabs this user may open; none means the settings are not offered.
func settingsSections(g *Guild, meID string) []guildSection {
	can := func(bit int64) bool { return has(g.Permissions, bit) }

	var list []guildSection
	add := func(show bool, key, label string) {
		if show {
			list = append(list, guildSection{key, label})
		}
	}

	add(can(permGuild), "overview", "Visão geral")
	add(can(permRoles), "roles", "Cargos")
	add(can(permRoles) || can(permKick) || can(permBan), "members", "Membros")
	add(can(permGuild), "invites", "Convites")
	add(can(permBan), "bans", "Banimentos")
	add(g.OwnerID == meID, "delete", "Excluir servidor")

	return list
}

func (d *Discordo) openServerSettings(g *Guild) {
	s := &d.spaces().server
	*s = serverSettings{open: true, guildID: g.ID, role: g.ID, icon: g.IconURL, banner: g.BannerURL}
	s.name.SetString(g.Name)
	d.spaces().err = ""
}

// serverSettingsDialog is ServerSettingsDialog.
func (d *Discordo) serverSettingsDialog() {
	k, c := d.k, d.k.Context()
	s := &d.spaces().server
	d.takePickedImage()

	if !s.open {
		return
	}

	g := d.st.Guild(s.guildID)
	if g == nil {
		s.open = false // deleted, or we left: GUILD_DELETE took it
		return
	}

	sections := settingsSections(g, d.st.Me.ID)

	dlg := d.dialog("server settings", &s.open, rexui.DialogOpts{Title: "Configurações do servidor", Width: 768})
	if dlg.Visible {
		d.para(g.Name, d.ui.s14, fog)

		if len(sections) == 0 {
			d.para("Você não tem permissão para mudar nada aqui.", d.ui.s14, greyple)
		} else {
			titles := make([]string, len(sections))
			for i, sec := range sections {
				titles[i] = sec.label
			}

			s.tab = min(s.tab, len(sections)-1)
			if k.Tabs("server tabs", &s.tab, titles).Changed {
				d.spaces().err = ""
			}

			// The dialog is max-h-[90vh]: its content scrolls past what the window holds.
			_, h := c.Size()
			maxH := max(float32(h)/k.Scale()*0.9-180, 200)
			section := sections[s.tab].key

			k.ScrollUpTo("server section", &s.scroll, base.ScrollVertical, maxH, func() {
				d.begin(section, box{axis: ui.AxisY, w: ui.Pct(1), t: 8, r: 8, gap: 16})

				switch section {
				case "overview":
					d.guildOverview(g)
				case "roles":
					d.guildRoles(g)
				case "members":
					d.guildMembers(g)
				case "invites":
					d.guildInvites(g)
				case "bans":
					d.guildBans(g)
				case "delete":
					d.guildDelete(g)
				}

				c.Pop()
			})
		}
	}

	rexui.EndDialog(k.Kit, &dlg)
}

// guildOverview is settings/overview.tsx.
func (d *Discordo) guildOverview(g *Guild) {
	k, c := d.k, d.k.Context()
	s := &d.spaces().server

	d.begin("name", box{axis: ui.AxisY, w: ui.Pct(1), gap: 8})
	d.guildLabel("NOME DO SERVIDOR")
	k.TextField("##guild settings name", &s.name, "")
	c.Pop()

	name := strings.TrimSpace(s.name.String())
	d.guildImagePicker("ÍCONE", &s.icon, false, initialsOf(name), "icon")
	d.guildImagePicker("BANNER", &s.banner, true, "", "banner")
	d.guildError()

	dirty := s.name.String() != g.Name || s.icon != g.IconURL || s.banner != g.BannerURL
	d.begin("save", box{axis: ui.AxisX})
	if guildButton(d, "Salvar alterações", rexui.ButtonPrimary, !dirty || name == "") {
		body := map[string]string{"name": name, "icon_url": s.icon, "banner_url": s.banner}
		guildCall(d, "PATCH", "/guilds/"+g.ID, body, func(*Discordo, struct{}) { s.name.SetString(name) })
	}
	c.Pop()
}

// guildImagePicker is ImagePicker: the picture (or the fallback), "Enviar imagem" and "Remover".
func (d *Discordo) guildImagePicker(label string, url *string, wide bool, fallback, which string) {
	k, c := d.k, d.k.Context()
	s := &d.spaces().server

	d.begin(which, box{axis: ui.AxisY, w: ui.Pct(1), gap: 8})
	d.guildLabel(label)
	d.begin("row", box{axis: ui.AxisX, gap: 12, cross: ui.CrossCenter})

	w, h, radius := float32(80), float32(80), float32(16)
	if wide {
		w, radius = 160, 8
	}

	d.begin("preview", box{w: ui.Px(d.px(w)), h: ui.Px(d.px(h)), radius: radius, bg: surfaceRaised, clip: true, cross: ui.CrossCenter, justify: ui.JustifyCenter})
	if hnd, st := d.image(*url, int32(d.px(w)), int32(d.px(h)), true); st == img.Ready {
		c.NextSize(ui.Pct(1), ui.Pct(1))
		k.Image(6, hnd, ui.FitFill, d.px(radius), "")
	} else if fallback != "" {
		d.text(fallback, d.ui.s18b, fog)
	}
	c.Pop()

	d.begin("buttons", box{axis: ui.AxisY, gap: 6})
	busy := s.uploading == which
	send := "Enviar imagem"
	if busy {
		send = "Enviando…"
	}

	if guildButton(d, send+"###send", rexui.ButtonDefault, busy || s.ticket != 0) && d.a != nil {
		t, err := d.a.OpenPanel(platform.OpenSpec{Files: true, Types: []string{".png", ".jpg", ".jpeg", ".gif", ".webp"}})
		if err != nil {
			d.spaces().err = "Não foi possível abrir o seletor de arquivos: " + err.Error()
		} else {
			s.ticket, s.picking = t, which
		}
	}

	if *url != "" && guildButton(d, "Remover", rexui.ButtonPlain, false) {
		*url = ""
	}
	c.Pop()

	c.Pop()
	c.Pop()
}

// takePickedImage takes the open panel's answer and uploads the picture (api.uploadImage).
func (d *Discordo) takePickedImage() {
	s := &d.spaces().server
	if s.ticket == 0 || d.a == nil {
		return
	}

	r, ok := d.a.TakeDialogFor(s.ticket)
	if !ok {
		return
	}

	s.ticket = 0
	if r.Outcome != platform.DialogAnswered || len(r.Paths) == 0 {
		return
	}

	path, which := r.Paths[0], s.picking
	client, gen := d.client, d.gen
	s.uploading, d.spaces().err = which, ""

	go func() {
		var out struct {
			URL string `json:"url"`
		}

		data, err := os.ReadFile(path)
		if err == nil {
			err = client.Upload(context.Background(), "/images", filepath.Base(path), data, &out)
		}

		d.post(func(d *Discordo) {
			if d.gen != gen {
				return
			}

			s.uploading = ""
			if err != nil {
				d.spaces().err = "Não foi possível enviar a imagem: " + err.Error()
				return
			}

			if which == "icon" {
				s.icon = out.URL
			} else {
				s.banner = out.URL
			}
		})
	}()
}

// roleRGBA is a role's colour (0xRRGGBB, 0 none) as a dot's: greyple when it has none.
func roleRGBA(color int) uint32 {
	if color == 0 {
		return greyple
	}

	return uint32(color)<<8 | 0xff
}

// guildRoles is settings/roles.tsx: the roles, highest first, and the selected one's editor.
func (d *Discordo) guildRoles(g *Guild) {
	c := d.k.Context()
	s := &d.spaces().server
	top := myTop(g, d.st.Me.ID)

	sorted := slices.Clone(g.Roles)
	slices.SortStableFunc(sorted, func(a, b Role) int { return b.Position - a.Position })

	i := slices.IndexFunc(sorted, func(r Role) bool { return r.ID == s.role })
	if i < 0 {
		i = slices.IndexFunc(sorted, func(r Role) bool { return r.ID == g.ID })
	}

	d.begin("roles", box{axis: ui.AxisX, w: ui.Pct(1), gap: 16})

	d.begin("list", box{axis: ui.AxisY, w: ui.Px(d.px(180)), gap: 8})
	d.begin("create", box{axis: ui.AxisX, w: ui.Pct(1)})
	d.k.NextWidth(ui.Pct(1))
	if guildButton(d, "Criar cargo", rexui.ButtonPrimary, false) {
		guildCall(d, "POST", "/guilds/"+g.ID+"/roles", map[string]any{}, func(d *Discordo, r Role) { s.role = r.ID })
	}
	c.Pop()

	d.begin("rows", box{axis: ui.AxisY, w: ui.Pct(1), gap: 2})
	for _, r := range sorted {
		name := r.Name
		if r.ID == g.ID {
			name = "@everyone"
		}

		if d.guildListRow(r.ID, name, i >= 0 && r.ID == sorted[i].ID, roleRGBA(r.Color)) {
			s.role, d.spaces().err = r.ID, ""
		}
	}
	c.Pop()
	c.Pop()

	if i >= 0 {
		movable := slices.DeleteFunc(slices.Clone(sorted), func(r Role) bool { return r.ID == g.ID })
		role := sorted[i]
		j := slices.IndexFunc(movable, func(r Role) bool { return r.ID == role.ID })
		above := j > 0 && movable[j-1].Position < top
		below := j >= 0 && j+1 < len(movable)

		d.begin("editor", box{axis: ui.AxisY, w: ui.Grow(1), gap: 16})
		d.roleEditor(g, &role, role.Position < top, above, below)
		c.Pop()
	}

	c.Pop()
}

// roleEditor is RoleEditor: name and colour (saved together), order, delete, and the permission switches.
func (d *Discordo) roleEditor(g *Guild, role *Role, editable, above, below bool) {
	k, c := d.k, d.k.Context()
	s := &d.spaces().server
	everyone := role.ID == g.ID

	// Another role was selected: fill the form from it.
	if s.roleFor != role.ID {
		s.roleFor, s.roleConfirm = role.ID, false
		s.roleName.SetString(role.Name)
		s.roleColor = 0
		if role.Color != 0 {
			s.roleColor = roleRGBA(role.Color)
		}
	}

	if !editable {
		d.begin("locked", box{axis: ui.AxisY, w: ui.Pct(1), bg: surfaceRaised, radius: 6, l: 12, r: 12, t: 8, b: 8})
		d.para("Este cargo está na sua posição ou acima dela: você não pode editá-lo.", d.ui.s12, greyple)
		c.Pop()
	}

	color := int(s.roleColor >> 8)
	name := strings.TrimSpace(s.roleName.String())

	d.begin("form", box{axis: ui.AxisX, w: ui.Pct(1), gap: 8, cross: ui.CrossEnd})
	k.BeginDisabled(!editable)

	if !everyone {
		d.begin("name", box{axis: ui.AxisY, w: ui.Grow(1), gap: 8})
		d.guildLabel("NOME")
		k.TextField("##role name", &s.roleName, "")
		c.Pop()
	}

	d.begin("color", box{axis: ui.AxisY, gap: 8})
	d.guildLabel("COR")
	d.begin("well", box{axis: ui.AxisX, gap: 4, cross: ui.CrossCenter})
	if rexui.ColorWell(k.Kit, "Cor", &s.well, &s.roleColor, rexui.ColorWellOpts{}).Changed {
		s.roleColor |= 0xff // the well may hand back a translucent colour: a role's is opaque
	}

	if color != 0 && d.ghost("x", "Sem cor", 16) && editable {
		s.roleColor = 0
	}
	c.Pop()
	c.Pop()

	k.EndDisabled()

	dirty := color != role.Color || (!everyone && s.roleName.String() != role.Name)
	if guildButton(d, "Salvar", rexui.ButtonPrimary, !editable || !dirty || (!everyone && name == "")) {
		body := map[string]any{"color": color}
		if !everyone {
			body["name"] = name
		}

		guildCall(d, "PATCH", "/guilds/"+g.ID+"/roles/"+role.ID, body, func(*Discordo, Role) { s.roleName.SetString(name) })
	}
	c.Pop()

	if !everyone {
		d.begin("order", box{axis: ui.AxisX, w: ui.Pct(1), gap: 8, cross: ui.CrossCenter})
		for _, mv := range [...]struct {
			label  string
			up, ok bool
		}{{"Subir", true, above}, {"Descer", false, below}} {
			if guildButton(d, mv.label, rexui.ButtonDefault, !editable || !mv.ok) {
				guildCall(d, "POST", "/guilds/"+g.ID+"/roles/"+role.ID+"/move", map[string]bool{"up": mv.up}, func(*Discordo, struct{}) {})
			}
		}

		d.spacer()

		if s.roleConfirm {
			if guildButton(d, "Confirmar exclusão", rexui.ButtonDestructive, false) {
				guildCall(d, "DELETE", "/guilds/"+g.ID+"/roles/"+role.ID, nil, func(*Discordo, struct{}) { s.role = g.ID })
			}
		} else if guildButton(d, "Excluir cargo", rexui.ButtonPlain, !editable) {
			s.roleConfirm = true
		}
		c.Pop()
	}

	d.guildError()

	d.begin("permissions", box{axis: ui.AxisY, w: ui.Pct(1), gap: 4})
	d.guildLabel("PERMISSÕES")
	for i, label := range permissionLabels {
		bit := int64(1) << i
		d.begin(label, box{axis: ui.AxisX, w: ui.Pct(1), bg: surfaceRaised, radius: 6, l: 12, r: 12, t: 8, b: 8, gap: 12, cross: ui.CrossCenter})
		d.text(label, d.ui.s14, fog)
		d.spacer()

		// Nobody grants or revokes what they do not hold.
		on := role.Permissions&bit != 0
		k.BeginDisabled(!editable || !has(g.Permissions, bit))
		if rexui.Switch(k.Kit, "##"+label, &on, rexui.SwitchOpts{}).Changed {
			perms := role.Permissions &^ bit
			if on {
				perms |= bit
			}

			guildCall(d, "PATCH", "/guilds/"+g.ID+"/roles/"+role.ID, map[string]int64{"permissions": perms}, func(*Discordo, Role) {})
		}
		k.EndDisabled()
		c.Pop()
	}
	c.Pop()
}

// guildMembers is settings/members.tsx: search, and each member's roles, kick and ban.
func (d *Discordo) guildMembers(g *Guild) {
	k := d.k
	s := &d.spaces().server

	k.SearchField("##member search", &s.search, "Procurar membro...")

	n := len(g.Members)
	count := itoa(n) + " membros"
	if n == 1 {
		count = "1 membro"
	}

	d.text(count, d.ui.s12, greyple)

	query := strings.ToLower(strings.TrimSpace(s.search.String()))
	shown := 0
	for i := range g.Members {
		m := &g.Members[i]
		if query != "" && !strings.Contains(strings.ToLower(m.Name()), query) && !strings.Contains(strings.ToLower(m.User.Username), query) {
			continue
		}

		shown++
		d.guildMemberRow(g, m)
	}

	if shown == 0 {
		d.begin("none", box{axis: ui.AxisY, w: ui.Pct(1), t: 32, b: 32, cross: ui.CrossCenter})
		d.text("Nenhum resultado", d.ui.s14, greyple)
		d.k.Context().Pop()
	}
}

// guildMemberRow is MemberRow.
func (d *Discordo) guildMemberRow(g *Guild, m *Member) {
	c := d.k.Context()
	s := &d.spaces().server
	me, id, name := d.st.Me.ID, m.User.ID, m.Name()
	top := myTop(g, me)
	isOwner := id == g.OwnerID
	actionable := !isOwner && id != me && myTop(g, id) < top
	canRoles := actionable && has(g.Permissions, permRoles)
	assignable := func(r Role) bool { return canRoles && r.ID != g.ID && r.Position < top }

	d.guildBox(id, box{axis: ui.AxisY, w: ui.Pct(1), bg: surfaceRaised, radius: 8, l: 12, r: 12, t: 12, b: 12, gap: 8}, line10)

	d.begin("head", box{axis: ui.AxisX, w: ui.Pct(1), gap: 12, cross: ui.CrossCenter})
	d.avatar(m.User, name, 32, rexui.PresenceNone, surfaceRaised)
	d.begin("names", box{axis: ui.AxisY, w: ui.Grow(1)})
	d.begin("name", box{axis: ui.AxisX, gap: 6, cross: ui.CrossCenter})
	d.text(name, d.ui.s14m, snow)
	if isOwner {
		d.icon("crown", 14, 0xfbbf24ff) // amber-400
	}
	c.Pop()
	d.text("@"+m.User.Username, d.ui.s12, greyple)
	c.Pop()

	switch {
	case actionable && s.punishing == id:
		if guildButton(d, "Cancelar", rexui.ButtonPlain, false) {
			s.punishing = ""
		}

		verb, method, path := "Expulsar ", "DELETE", "/guilds/"+g.ID+"/members/"+id
		if s.punishBan {
			verb, method, path = "Banir ", "PUT", "/guilds/"+g.ID+"/bans/"+id
		}

		if guildButton(d, verb+name, rexui.ButtonDestructive, false) {
			guildCall(d, method, path, nil, func(*Discordo, struct{}) { s.punishing = "" })
		}

	case actionable:
		if has(g.Permissions, permKick) && d.guildDanger("user-minus", "Expulsar "+name) {
			s.punishing, s.punishBan = id, false
		}

		if has(g.Permissions, permBan) && d.guildDanger("ban", "Banir "+name) {
			s.punishing, s.punishBan = id, true
		}
	}
	c.Pop()

	var addable []Role
	held := 0
	for _, r := range g.Roles {
		if slices.Contains(m.Roles, r.ID) {
			held++
		} else if assignable(r) {
			addable = append(addable, r)
		}
	}

	if held+len(addable) > 0 {
		c.BeginFlow("roles", ui.FlowOpts{GapX: d.px(4), GapY: d.px(4), Cross: ui.CrossCenter})
		for _, r := range g.Roles {
			if !slices.Contains(m.Roles, r.ID) {
				continue
			}

			d.guildBox(r.ID, box{axis: ui.AxisX, radius: 6, l: 6, r: 6, t: 2, b: 2, gap: 4, cross: ui.CrossCenter}, line10)
			d.begin("dot", box{w: ui.Px(d.px(8)), h: ui.Px(d.px(8)), radius: 4, bg: roleRGBA(r.Color)})
			c.Pop()
			d.text(r.Name, d.ui.s12, fog)

			if assignable(r) && d.guildIconPress("x", "Tirar o cargo "+r.Name, 12, greyple) {
				guildCall(d, "DELETE", "/guilds/"+g.ID+"/members/"+id+"/roles/"+r.ID, nil, func(*Discordo, struct{}) {})
			}
			c.Pop()
		}

		if len(addable) > 0 {
			pill := d.guildPill("Cargo", "plus")
			if i, ok := rexui.DropdownMenu(d.k.Kit, pill, func(items *[]ui.MenuItem) {
				for i, r := range addable {
					*items = append(*items, ui.MenuItem{ID: i, Title: r.Name})
				}
			}); ok {
				guildCall(d, "PUT", "/guilds/"+g.ID+"/members/"+id+"/roles/"+addable[i].ID, nil, func(*Discordo, struct{}) {})
			}
		}
		c.EndFlow()
	}

	c.Pop()
}

// guildDanger is the web's ghost icon-sm button in the destructive colour.
func (d *Discordo) guildDanger(icon, label string) bool {
	c := d.k.Context()
	d.press(ui.HashString(c.ParentKey(), label), box{w: ui.Px(d.px(32)), h: ui.Px(d.px(32)), radius: 8, cross: ui.CrossCenter, justify: ui.JustifyCenter}, alpha(red, 0.15), label)
	d.icon(icon, 16, red)
	id := c.Parent()
	clicked := d.pop()
	rexui.Tooltip(d.k.Kit, id, label)

	return clicked
}

// guildFetch loads the open tab's list (invites or bans) once; a change sets fetched to "" to load it again.
func (d *Discordo) guildFetch(g *Guild, section string) {
	s := &d.spaces().server
	if s.fetched == section {
		return
	}

	s.fetched, s.loaded = section, false

	if section == "invites" {
		guildCall(d, "GET", "/guilds/"+g.ID+"/invites", nil, func(d *Discordo, l []Invite) { s.invites, s.loaded = l, true })
	} else {
		guildCall(d, "GET", "/guilds/"+g.ID+"/bans", nil, func(d *Discordo, l []Ban) { s.bans, s.loaded = l, true })
	}
}

// guildListState is ListState: a spinner, the error, or the empty line. It reports whether the list shows.
func (d *Discordo) guildListState(n int, empty string) bool {
	c := d.k.Context()
	s := &d.spaces().server

	switch {
	case !s.loaded && d.spaces().err == "":
		d.begin("loading", box{axis: ui.AxisY, w: ui.Pct(1), t: 32, b: 32, cross: ui.CrossCenter})
		rexui.Spinner(d.k.Kit, "Carregando", rexui.SpinnerOpts{})
		c.Pop()
	case !s.loaded:
		d.guildError()
	case n == 0:
		d.begin("empty", box{axis: ui.AxisY, w: ui.Pct(1), t: 32, b: 32, cross: ui.CrossCenter})
		d.text(empty, d.ui.s14, greyple)
		c.Pop()
	default:
		d.guildError()
		return true
	}

	return false
}

// guildInvites is settings/invites.tsx's Invites.
func (d *Discordo) guildInvites(g *Guild) {
	c := d.k.Context()
	s := &d.spaces().server
	d.guildFetch(g, "invites")

	if !d.guildListState(len(s.invites), "Nenhum convite ativo") {
		return
	}

	for _, inv := range s.invites {
		creator := "Desconhecido"
		if inv.Creator != nil {
			creator = inv.Creator.Name()
		}

		max := "∞"
		if inv.MaxUses > 0 {
			max = itoa(inv.MaxUses)
		}

		expires := "não expira"
		if inv.ExpiresAt != nil {
			t := inv.ExpiresAt.Local()
			expires = "expira " + t.Format("02") + " de " + months[t.Month()-1] + ", " + t.Format("15:04")
		}

		d.guildBox(inv.Code, box{axis: ui.AxisX, w: ui.Pct(1), bg: surfaceRaised, radius: 8, l: 12, r: 12, t: 12, b: 12, gap: 12, cross: ui.CrossCenter}, line10)
		if inv.Creator != nil {
			d.avatar(*inv.Creator, creator, 32, rexui.PresenceNone, surfaceRaised)
		}

		d.begin("words", box{axis: ui.AxisY, w: ui.Grow(1)})
		d.text(inv.Code, d.ui.s14, blurpleSoft)
		d.text(creator+" · "+itoa(inv.Uses)+"/"+max+" usos · "+expires, d.ui.s12, greyple)
		c.Pop()

		link := d.inviteLink(inv.Code)
		icon, label := "copy", "Copiar link"
		if d.spaces().copied == link {
			icon, label = "check", "Copiado"
		}

		if d.ghost(icon, label+"###copy", 16) {
			d.guildCopy(link)
		}

		if d.guildDanger("trash-2", "Revogar") {
			guildCall(d, "DELETE", "/invites/"+inv.Code, nil, func(*Discordo, struct{}) { s.fetched = "" })
		}
		c.Pop()
	}
}

// guildBans is settings/invites.tsx's Bans.
func (d *Discordo) guildBans(g *Guild) {
	c := d.k.Context()
	s := &d.spaces().server
	d.guildFetch(g, "bans")

	if !d.guildListState(len(s.bans), "Ninguém está banido") {
		return
	}

	for _, b := range s.bans {
		u := b.User
		d.guildBox(u.ID, box{axis: ui.AxisX, w: ui.Pct(1), bg: surfaceRaised, radius: 8, l: 12, r: 12, t: 12, b: 12, gap: 12, cross: ui.CrossCenter}, line10)
		d.avatar(u, u.Name(), 32, rexui.PresenceNone, surfaceRaised)
		d.begin("words", box{axis: ui.AxisY, w: ui.Grow(1)})
		d.text(u.Name(), d.ui.s14m, snow)
		d.text("@"+u.Username, d.ui.s12, greyple)
		c.Pop()

		if guildButton(d, "Desbanir", rexui.ButtonDefault, false) {
			guildCall(d, "DELETE", "/guilds/"+g.ID+"/bans/"+u.ID, nil, func(*Discordo, struct{}) { s.fetched = "" })
		}
		c.Pop()
	}
}

// guildDelete is settings/delete-guild.tsx: type the name to delete; GUILD_DELETE then takes everyone home.
func (d *Discordo) guildDelete(g *Guild) {
	k, c := d.k, d.k.Context()
	s := &d.spaces().server

	d.para("Isso apaga o servidor, todos os canais e mensagens. Não dá para desfazer.", d.ui.s14, fog)

	d.begin("confirm", box{axis: ui.AxisY, w: ui.Pct(1), gap: 8})
	d.begin("label", box{axis: ui.AxisX})
	d.guildLabel("DIGITE ")
	d.text(strings.ToUpper(g.Name), d.ui.caption, snow)
	d.guildLabel(" PARA CONFIRMAR")
	c.Pop()
	k.TextField("##delete guild name", &s.typed, "")
	c.Pop()

	d.guildError()

	d.begin("delete", box{axis: ui.AxisX})
	if guildButton(d, "Excluir servidor", rexui.ButtonDestructive, s.typed.String() != g.Name) {
		guildCall(d, "DELETE", "/guilds/"+g.ID, nil, func(*Discordo, struct{}) {})
	}
	c.Pop()
}
