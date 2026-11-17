package main

// Home: the DM sidebar (home-sidebar.tsx), the friends (friends-view.tsx) and groups.

import (
	"slices"
	"strings"

	"gonativeui/rexui"
	"gonativeui/ui"
	"gonativeui/widget/base"
)

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
	d.spacer()

	if d.clicked(d.iconButton("plus", "Criar grupo", 24)) {
		d.openFriendPicker("")
	}
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

		if d.inCall(dm.ID) {
			id := d.icon("volume-2", 16, green)
			rexui.Tooltip(k.Kit, id, "Chamada em andamento")
		}

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

func (d *Discordo) incoming() int {
	n := 0
	for _, r := range d.st.Relationships {
		if r.Status == 2 {
			n++
		}
	}

	return n
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

	// Who they are is a button: their profile card.
	d.press(ui.HashString(c.ParentKey(), "profile"), box{axis: ui.AxisX, w: ui.Grow(1), gap: 12, cross: ui.CrossCenter}, 0, "Abrir perfil de "+u.Name())

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

	if d.pop() {
		d.openProfile(u.ID)
	}

	d.begin("actions", box{axis: ui.AxisX, gap: 4})
	switch r.Status {
	case 1:
		if d.secondary("message-circle", "Enviar mensagem para "+u.Name(), 0) {
			call(d, "POST", "/dms", map[string]string{"recipient_id": u.ID}, func(d *Discordo, dm DM) { d.open("", dm.ID) })
		}

		if d.secondary("user-minus", "Desfazer amizade com "+u.Name(), 0) {
			d.confirm("Desfazer amizade com "+u.Name()+"?", "Vocês deixam de ser amigos. Para voltar, um dos dois precisa enviar outro pedido.",
				"Desfazer amizade", "DELETE", "/relationships/"+u.ID)
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

// addFriendForm is AddFriend: a username, and the request.
func (d *Discordo) addFriendForm() {
	k, c := d.k, d.k.Context()
	d.begin("add", box{axis: ui.AxisY, w: ui.Pct(1), l: 28, r: 28, t: 20, b: 20, gap: 4})
	d.text("Adicionar amigo", d.ui.s18b, snow)
	d.text("Envie um pedido de amizade pelo nome de usuário.", d.ui.s14, fog)

	c.NextMaxSize(d.px(768), 0)
	d.begin("form", box{axis: ui.AxisX, w: ui.Pct(1), t: 12, gap: 8, cross: ui.CrossCenter})
	k.NextWidth(ui.Grow(1))
	f := k.TextField("##username", &d.ui.addFriend, "nome de usuário")

	k.BeginDisabled(strings.TrimSpace(d.ui.addFriend.String()) == "")
	send := rexui.Button(k.Kit, "Enviar pedido", rexui.ButtonOpts{Variant: rexui.ButtonPrimary}).Clicked
	k.EndDisabled()
	c.Pop()

	if (send || f.Submitted) && strings.TrimSpace(d.ui.addFriend.String()) != "" {
		name := strings.TrimPrefix(strings.TrimSpace(d.ui.addFriend.String()), "@")
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

// groupMax is home-sidebar.tsx's GROUP_MAX: the most people in a group, me included.
const groupMax = 10

// soc is home's dialog state: the shared Discordo struct is frozen, and there is one window.
var soc struct {
	picker  bool
	group   string // the group the picker adds to; "" creates one
	picked  []string
	name    ui.TextBuffer
	scroll  base.ScrollState
	pending bool

	asking                   bool
	title, desc, label       string
	method, path, afterLeave string
	busy                     bool
}

func (d *Discordo) inCall(channelID string) bool {
	for _, v := range d.st.Voice {
		if v.ChannelID == channelID {
			return true
		}
	}

	return false
}

// socialDialogs draws home's dialogs: new group, add to a group, the confirmations.
func (d *Discordo) socialDialogs() {
	if d.phase != phaseOnline || !d.ready {
		soc.picker, soc.asking = false, false
		return
	}

	d.friendPicker()
	d.confirmDialog()
}

// dmHeaderActions are the buttons at the right of a DM's or group's header (home-content.tsx's DMView).
func (d *Discordo) dmHeaderActions(dm *DM) {
	// The call: start it or leave it (the stage of a DM call is not drawn yet; the dock shows it).
	if vc.channel == dm.ID {
		if d.ghost("phone-off", "Sair da chamada", 16) {
			d.voiceLeave()
		}
	} else if d.ghost("phone", "Iniciar chamada", 16) {
		d.voiceJoin(dm.ID, -1)
	}

	if dm.Type != ChannelGroup {
		return
	}

	c := d.k.Context()
	if len(dm.Recipients) >= groupMax {
		d.begin("full", box{w: ui.Px(d.px(32)), h: ui.Px(d.px(32)), cross: ui.CrossCenter, justify: ui.JustifyCenter})
		d.icon("user-plus", 16, alpha(fog, 0.4))
		id := c.Parent()
		c.Pop()
		rexui.Tooltip(d.k.Kit, id, "Adicionar membro")
	} else if d.ghost("user-plus", "Adicionar membro", 16) {
		d.openFriendPicker(dm.ID)
	}

	if d.ghost("log-out", "Sair do grupo", 16) {
		title := d.st.DMName(*dm)
		d.confirm("Sair de "+title+"?", "Você deixa de ver as mensagens do grupo até alguém adicionar você de novo.",
			"Sair do grupo", "DELETE", "/dms/"+dm.ID+"/recipients/@me")
		soc.afterLeave = dm.ID
	}
}

// openFriendPicker opens dialogs.tsx's FriendPicker: a new group (groupID ""), or more people for one.
func (d *Discordo) openFriendPicker(groupID string) {
	soc.picker, soc.group, soc.picked, soc.pending = true, groupID, nil, false
	soc.name.SetString("")
	d.status = ""
}

// pickable are the friends the picker offers: not in the group already, by name.
func pickable(rels []Relationship, exclude []User) []User {
	var list []User
	for _, r := range rels {
		if r.Status == 1 && !slices.ContainsFunc(exclude, func(u User) bool { return u.ID == r.User.ID }) {
			list = append(list, r.User)
		}
	}

	slices.SortFunc(list, func(a, b User) int { return strings.Compare(strings.ToLower(a.Name()), strings.ToLower(b.Name())) })

	return list
}

// friendPicker is dialogs.tsx's FriendPicker, as home-sidebar.tsx (Criar grupo) and DMView (Adicionar membro) set it.
func (d *Discordo) friendPicker() {
	k, c := d.k, d.k.Context()
	group := d.st.DM(soc.group)
	if soc.group != "" && group == nil {
		soc.picker = false
	}

	title, desc, submit, limit := "Criar grupo", "Escolha amigos para a conversa em grupo.", "Criar grupo", groupMax-1
	var exclude []User
	if group != nil {
		name := d.st.DMName(*group)
		title, desc, submit, limit = "Adicionar membro", "Adicione amigos a "+name+".", "Adicionar", groupMax-len(group.Recipients)
		exclude = group.Recipients
	}

	dlg := rexui.BeginDialog(k.Kit, "friend picker", &soc.picker, rexui.DialogOpts{Title: title, Width: 448, DimCancels: true})
	if !dlg.Visible {
		rexui.EndDialog(k.Kit, &dlg)
		return
	}

	d.para(desc+" ("+itoa(len(soc.picked))+"/"+itoa(limit)+")", d.ui.s14, fog)

	friends := pickable(d.st.Relationships, exclude)
	if len(friends) == 0 {
		d.begin("none", box{axis: ui.AxisY, w: ui.Pct(1), t: 24, b: 24, cross: ui.CrossCenter})
		d.text("Nenhum amigo disponível.", d.ui.s14, greyple)
		c.Pop()
	} else {
		k.NextSize(ui.Pct(1), ui.Px(min(float32(len(friends))*d.px(46), d.px(288))))
		v := k.BeginScroll("friends", &soc.scroll, base.ScrollVertical)
		d.begin("list", box{axis: ui.AxisY, w: ui.Pct(1), r: 8, gap: 2})

		for _, u := range friends {
			on := slices.Contains(soc.picked, u.ID)
			off := !on && len(soc.picked) >= limit
			hover := uint32(surfaceHover)
			if off {
				hover = 0
			}

			d.press(ui.HashString(c.ParentKey(), u.ID), box{axis: ui.AxisX, w: ui.Pct(1), h: ui.Px(d.px(44)), l: 8, r: 8, gap: 12, radius: 8, cross: ui.CrossCenter}, hover, u.Name())
			d.avatar(u, u.Name(), 32, rexui.PresenceNone, surfaceRaised)
			color := uint32(snow)
			if off {
				color = alpha(snow, 0.5)
			}

			d.begin("name", box{w: ui.Grow(1)})
			d.text(u.Name(), d.ui.s14m, color)
			c.Pop()

			// The checkbox: accent-blurple when picked.
			check := box{w: ui.Px(d.px(16)), h: ui.Px(d.px(16)), radius: 4, cross: ui.CrossCenter, justify: ui.JustifyCenter}
			if on {
				check.bg = blurple
			} else {
				outline(c, alpha(greyple, 0.8), d.px(1))
			}

			d.begin("check", check)
			if on {
				d.icon("check", 12, white)
			}
			c.Pop()

			if d.pop() && !off {
				soc.picked = togglePick(soc.picked, u.ID)
			}
		}

		c.Pop()
		k.CloseScrollContent(&v)
		rexui.ScrollBars(k.Kit, &v)
		k.CloseScroll(&v)
	}

	if group == nil {
		d.begin("group name", box{axis: ui.AxisY, w: ui.Pct(1), t: 8, gap: 8})
		d.text("NOME DO GRUPO (OPCIONAL)", d.ui.caption, greyple)
		k.TextField("##group-name", &soc.name, "")
		clampRunes(&soc.name, 100)
		c.Pop()
	}

	if d.status != "" {
		d.para(d.status, d.ui.s14, red)
	}

	dlg.BeginActions()
	dlg.Action("Cancelar", rexui.ButtonDefault)

	k.BeginDisabled(len(soc.picked) == 0 || soc.pending)
	send := rexui.Button(k.Kit, submit, rexui.ButtonOpts{Variant: rexui.ButtonPrimary, Size: rexui.DialogLook.ButtonSize}).Clicked
	k.EndDisabled()
	dlg.EndActions()

	if send && len(soc.picked) > 0 {
		soc.pending = true
		end := func(d *Discordo) { soc.pending = false }

		if group != nil {
			callThen(d, "POST", "/dms/"+group.ID+"/recipients", map[string]any{"recipient_ids": soc.picked}, func(d *Discordo, _ struct{}) {
				soc.picker = false
			}, end)
		} else {
			body := map[string]any{"recipient_ids": soc.picked}
			if name := strings.TrimSpace(soc.name.String()); name != "" {
				body["name"] = name
			}

			callThen(d, "POST", "/dms/groups", body, func(d *Discordo, dm DM) {
				soc.picker = false
				d.open("", dm.ID)
			}, end)
		}
	}

	rexui.EndDialog(k.Kit, &dlg)
}

func togglePick(picked []string, id string) []string {
	if i := slices.Index(picked, id); i >= 0 {
		return slices.Delete(picked, i, i+1)
	}

	return append(picked, id)
}

// confirm opens confirm-dialog.tsx's ConfirmDialog for a destructive call.
func (d *Discordo) confirm(title, desc, label, method, path string) {
	soc.asking, soc.title, soc.desc, soc.label = true, title, desc, label
	soc.method, soc.path, soc.afterLeave, soc.busy = method, path, "", false
	d.status = ""
}

// confirmDialog is ConfirmDialog: it stays open, showing the error, when the call fails.
func (d *Discordo) confirmDialog() {
	k := d.k
	dlg := rexui.BeginDialog(k.Kit, "confirm", &soc.asking, rexui.DialogOpts{Title: soc.title, Width: 384, DimCancels: true})

	if dlg.Visible {
		d.para(soc.desc, d.ui.s14, fog)

		if d.status != "" {
			d.para(d.status, d.ui.s14, red)
		}

		dlg.BeginActions()
		dlg.Action("Cancelar", rexui.ButtonDefault)

		k.BeginDisabled(soc.busy)
		yes := rexui.Button(k.Kit, soc.label, rexui.ButtonOpts{Variant: rexui.ButtonDestructive, Size: rexui.DialogLook.ButtonSize}).Clicked
		k.EndDisabled()
		dlg.EndActions()

		if yes {
			soc.busy = true
			left := soc.afterLeave
			callThen(d, soc.method, soc.path, nil, func(d *Discordo, _ struct{}) {
				soc.asking = false
				if left != "" && d.channel == left {
					d.open("", "")
				}
			}, func(*Discordo) { soc.busy = false })
		}
	}

	rexui.EndDialog(k.Kit, &dlg)
}
