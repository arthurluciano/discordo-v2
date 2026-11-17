package main

// A channel's settings (channel-settings-dialog.tsx): rename, topic, delete, and its permission overwrites.

import (
	"slices"
	"strings"

	"gonativeui/rexui"
	"gonativeui/ui"
	"gonativeui/widget/base"
)

type channelSettings struct {
	open            bool
	guildID, id     string
	tab             int
	name, topic     ui.TextBuffer
	confirming      bool
	target          Overwrite   // the role or member whose overwrite is shown (TargetID, IsMember)
	added           []Overwrite // targets added from "Adicionar…" that have no overwrite yet
	scroll, targets base.ScrollState
}

// channelScoped is lib/permissions.ts's CHANNEL_SCOPED: the bits an overwrite may touch.
const channelScoped = permView | permSend | permManageMessages | permAttach | permReact | permMentionAll |
	permConnect | permSpeak | permStream | permManage | permRoles

// The overwrite tri-state (logic.ts's TriState), in the order of the web's STATES buttons.
const (
	triDeny = iota
	triInherit
	triAllow
)

// bitState is logic.ts's: where a bit stands in an overwrite (nil: none).
func bitState(o *Overwrite, bit int64) int {
	switch {
	case o == nil:
		return triInherit
	case o.Allow&bit != 0:
		return triAllow
	case o.Deny&bit != 0:
		return triDeny
	}

	return triInherit
}

// setBitState is logic.ts's: the overwrite's allow and deny with bit moved to state.
func setBitState(o *Overwrite, bit int64, state int) (allow, deny int64) {
	if o != nil {
		allow, deny = o.Allow&^bit, o.Deny&^bit
	}

	switch state {
	case triAllow:
		allow |= bit
	case triDeny:
		deny |= bit
	}

	return allow, deny
}

func (d *Discordo) openChannelSettings(g *Guild, ch *Channel) {
	s := &d.spaces().channel
	*s = channelSettings{open: true, guildID: g.ID, id: ch.ID, target: Overwrite{TargetID: g.ID}}
	s.name.SetString(ch.Name)
	s.topic.SetString(ch.Topic)
	d.spaces().err = ""
}

// overwriteTargetName is targetName: a member's name, a role's, or @everyone.
func overwriteTargetName(g *Guild, t Overwrite) string {
	if t.IsMember {
		if i := slices.IndexFunc(g.Members, func(m Member) bool { return m.User.ID == t.TargetID }); i >= 0 {
			return g.Members[i].Name()
		}

		return "Membro"
	}

	if t.TargetID == g.ID {
		return "@everyone"
	}

	if i := slices.IndexFunc(g.Roles, func(r Role) bool { return r.ID == t.TargetID }); i >= 0 {
		return g.Roles[i].Name
	}

	return "Cargo"
}

// channelSettingsDialog is ChannelSettingsDialog.
func (d *Discordo) channelSettingsDialog() {
	k := d.k
	s := &d.spaces().channel
	if !s.open {
		return
	}

	// Deleted, or we lost sight of it: the dialog goes.
	g := d.st.Guild(s.guildID)
	var ch *Channel
	if g != nil {
		ch = g.Channel(s.id)
	}

	if ch == nil {
		s.open = false
		return
	}

	dlg := rexui.BeginDialog(k.Kit, "channel settings", &s.open, rexui.DialogOpts{Title: "Configurações de " + ch.Name, Width: 672})
	if dlg.Visible {
		d.para("Nome, tópico e quem pode fazer o quê neste canal.", d.ui.s14, fog)

		if has(ch.Permissions, permRoles) {
			k.Tabs("channel tabs", &s.tab, []string{"Visão geral", "Permissões"})
		} else {
			s.tab = 0
		}

		if s.tab == 0 {
			d.channelOverview(ch)
		} else {
			d.channelOverwrites(g, ch)
		}
	}

	rexui.EndDialog(k.Kit, &dlg)
}

// channelOverview is the Overview tab: name, topic (text channels), save, delete.
func (d *Discordo) channelOverview(ch *Channel) {
	k, c := d.k, d.k.Context()
	s := &d.spaces().channel

	d.begin("overview", box{axis: ui.AxisY, w: ui.Pct(1), gap: 16, t: 8})

	d.begin("name", box{axis: ui.AxisY, w: ui.Pct(1), gap: 8})
	d.guildLabel("NOME DO CANAL")
	k.TextField("##channel settings name", &s.name, "")
	c.Pop()

	// shortcut: the web's topic is a textarea; a one-line field until the native text area is set up here.
	if ch.Type == ChannelText {
		d.begin("topic", box{axis: ui.AxisY, w: ui.Pct(1), gap: 8})
		d.guildLabel("TÓPICO")
		k.TextField("##channel settings topic", &s.topic, "Do que se fala aqui?")
		c.Pop()
	}

	d.guildError()

	name, topic := strings.TrimSpace(s.name.String()), strings.TrimSpace(s.topic.String())
	dirty := s.name.String() != ch.Name || s.topic.String() != ch.Topic

	d.begin("actions", box{axis: ui.AxisX, w: ui.Pct(1), gap: 8, cross: ui.CrossCenter})
	if guildButton(d, "Salvar", rexui.ButtonPrimary, !dirty || name == "") {
		guildCall(d, "PATCH", "/channels/"+ch.ID, map[string]string{"name": name, "topic": topic}, func(d *Discordo, _ Channel) {
			s.name.SetString(name)
			s.topic.SetString(topic)
		})
	}

	d.spacer()

	switch {
	case ch.Type == ChannelWorld: // the server refuses to delete the world
	case s.confirming:
		if guildButton(d, "Cancelar", rexui.ButtonPlain, false) {
			s.confirming = false
		}

		if guildButton(d, "Excluir de vez", rexui.ButtonDestructive, false) {
			guildCall(d, "DELETE", "/channels/"+ch.ID, nil, func(*Discordo, struct{}) { s.open = false })
		}
	case guildButton(d, "Excluir canal", rexui.ButtonPlain, false):
		s.confirming = true
	}
	c.Pop()

	c.Pop()
}

// channelOverwrites is the Overwrites tab: the roles and members with an overwrite, and the selected one's bits.
func (d *Discordo) channelOverwrites(g *Guild, ch *Channel) {
	c := d.k.Context()
	s := &d.spaces().channel

	targets := []Overwrite{{TargetID: g.ID}}
	for _, t := range append(slices.Clone(ch.Overwrites), s.added...) {
		if !slices.ContainsFunc(targets, func(x Overwrite) bool { return x.TargetID == t.TargetID }) {
			targets = append(targets, Overwrite{TargetID: t.TargetID, IsMember: t.IsMember})
		}
	}

	var overwrite *Overwrite
	if i := slices.IndexFunc(ch.Overwrites, func(o Overwrite) bool { return o.TargetID == s.target.TargetID }); i >= 0 {
		overwrite = &ch.Overwrites[i]
	}

	d.begin("overwrites", box{axis: ui.AxisX, w: ui.Pct(1), gap: 16, t: 8})

	// The targets, and "Adicionar…" for the roles and members without one.
	d.begin("targets", box{axis: ui.AxisY, w: ui.Px(d.px(180)), gap: 8})
	for _, t := range targets {
		on := t.TargetID == s.target.TargetID
		if d.guildListRow(t.TargetID, overwriteTargetName(g, t), on, 0) {
			s.target = t
		}
	}

	var addable []Overwrite
	for _, r := range g.Roles {
		if r.ID != g.ID && !slices.ContainsFunc(targets, func(x Overwrite) bool { return x.TargetID == r.ID }) {
			addable = append(addable, Overwrite{TargetID: r.ID})
		}
	}

	roles := len(addable)
	for _, m := range g.Members {
		if !slices.ContainsFunc(targets, func(x Overwrite) bool { return x.TargetID == m.User.ID }) {
			addable = append(addable, Overwrite{TargetID: m.User.ID, IsMember: true})
		}
	}

	if len(addable) > 0 {
		id := d.guildPill("Adicionar…", "plus")
		if i, ok := rexui.DropdownMenu(d.k.Kit, id, func(items *[]ui.MenuItem) {
			for i, t := range addable {
				if i == 0 && roles > 0 {
					*items = append(*items, ui.MenuItem{Title: "Cargos", Header: true})
				}

				if i == roles {
					*items = append(*items, ui.MenuItem{Title: "Membros", Header: true})
				}

				*items = append(*items, ui.MenuItem{ID: i, Title: overwriteTargetName(g, t)})
			}
		}); ok {
			s.added = append(s.added, addable[i])
			s.target = addable[i]
		}
	}
	c.Pop()

	// The selected target's bits, each deny / inherit / allow.
	d.begin("bits", box{axis: ui.AxisY, w: ui.Grow(1), gap: 8})
	d.begin("head", box{axis: ui.AxisX, w: ui.Pct(1), h: ui.Px(d.px(28)), cross: ui.CrossCenter})
	d.text(overwriteTargetName(g, s.target), d.ui.s14s, snow)
	d.spacer()
	if overwrite != nil && guildButton(d, "Limpar", rexui.ButtonPlain, false) {
		guildCall(d, "DELETE", "/channels/"+ch.ID+"/overwrites/"+s.target.TargetID, nil, func(*Discordo, struct{}) {})
	}
	c.Pop()

	d.guildError()

	target := s.target
	d.k.ScrollUpTo("bit list", &s.scroll, base.ScrollVertical, 320, func() {
		d.begin("rows", box{axis: ui.AxisY, w: ui.Pct(1), gap: 4, r: 4})

		for i, label := range permissionLabels {
			bit := int64(1) << i
			if bit&channelScoped == 0 {
				continue
			}

			d.begin(label, box{axis: ui.AxisX, w: ui.Pct(1), bg: surfaceRaised, radius: 6, l: 12, r: 12, t: 6, b: 6, gap: 2, cross: ui.CrossCenter})
			d.text(label, d.ui.s14, fog)
			d.spacer()

			current := bitState(overwrite, bit)
			for state, look := range overwriteStates {
				if d.overwriteButton(look.label, look.icon, state == current, look.on, look.text, has(ch.Permissions, bit)) && state != current {
					allow, deny := setBitState(overwrite, bit, state)
					path := "/channels/" + ch.ID + "/overwrites/" + target.TargetID

					if allow|deny != 0 {
						guildCall(d, "PUT", path, map[string]any{"is_member": target.IsMember, "allow": allow, "deny": deny}, func(*Discordo, struct{}) {})
					} else {
						guildCall(d, "DELETE", path, nil, func(*Discordo, struct{}) {})
					}
				}
			}

			c.Pop()
		}

		c.Pop()
	})
	c.Pop()

	c.Pop()
}

// overwriteStates are the web's STATES: Negar (red), Herdar, Permitir (green).
var overwriteStates = [...]struct {
	label, icon string
	on, text    uint32
}{{"Negar", "x", 0xf23f42ff, 0xffffffff}, {"Herdar", "slash", 0x404249ff, 0xf2f3f5ff}, {"Permitir", "check", 0x23a55aff, 0x000000ff}}

// overwriteButton is one of a bit's three 28 pt radio buttons.
func (d *Discordo) overwriteButton(label, icon string, on bool, onBG, onText uint32, enabled bool) bool {
	c := d.k.Context()
	b := box{w: ui.Px(d.px(28)), h: ui.Px(d.px(28)), radius: 6, cross: ui.CrossCenter, justify: ui.JustifyCenter}
	color := uint32(greyple)
	if on {
		b.bg, color = onBG, onText
	}

	hover := uint32(surfaceHover)
	if on || !enabled {
		hover = 0
	}

	if !enabled {
		color = alpha(color, 0.4)
	}

	d.press(ui.HashString(c.ParentKey(), label), b, hover, label)
	d.icon(icon, 14, color)
	id := c.Parent()
	clicked := d.pop()
	rexui.Tooltip(d.k.Kit, id, label)

	return clicked && enabled
}

// guildListRow is a row of a settings list (targets, roles): greyple, snow and surface-active when selected.
// dot, when not 0, is a colour dot before the name (a role's colour).
func (d *Discordo) guildListRow(key, name string, on bool, dot uint32) bool {
	c := d.k.Context()
	bg := uint32(0)
	if on {
		bg = surfaceActive
	}

	hot := d.press(ui.HashString(c.ParentKey(), key), box{axis: ui.AxisX, w: ui.Pct(1), l: 8, r: 8, t: 6, b: 6, gap: 8, radius: 6, bg: bg, cross: ui.CrossCenter}, surfaceHover, name)
	color := uint32(greyple)
	if on || hot {
		color = snow
	}

	if dot != 0 {
		d.begin("dot", box{w: ui.Px(d.px(10)), h: ui.Px(d.px(10)), radius: 5, bg: dot})
		c.Pop()
	}

	d.text(name, d.ui.s14, color)

	return d.pop()
}

// guildPill is a small bordered trigger (a select's look) for a DropdownMenu; it returns its box.
func (d *Discordo) guildPill(label, icon string) ui.ID {
	c := d.k.Context()
	hot := d.guildPress(ui.HashString(c.ParentKey(), label), box{axis: ui.AxisX, h: ui.Px(d.px(28)), l: 8, r: 8, gap: 4, radius: 6, cross: ui.CrossCenter}, surfaceHover, line10, label)
	color := uint32(fog)
	if hot {
		color = snow
	}

	d.icon(icon, 12, color)
	d.text(label, d.ui.s12, color)
	id := c.Parent()
	c.Pop()

	return id
}
