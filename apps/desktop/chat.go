package main

// The chat: header, transcript, messages, composer and member list (features/chat, features/members).

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gonativeui/app"
	"gonativeui/img"
	"gonativeui/platform"
	"gonativeui/rexui"
	"gonativeui/theme"
	"gonativeui/ui"
	"gonativeui/widget"
	"gonativeui/widget/base"
)

// chatX is the chat's own state between frames: the shared structs (main.go, kit.go) are not the chat's to grow.
type chatX struct {
	channel string // the chat the rest belongs to: another chat starts over

	// The message being edited (message-item.tsx Editor): a second composer, made at a safe point.
	edit                      *rexui.ComposerState
	making                    bool
	editID, editOrig, editErr string
	editFocus                 bool

	// The files of the next message (composer.tsx useUploads).
	uploads []chatUpload
	nextKey int
	panel   platform.DialogTicket                // the open panel of the paperclip, 0 for none
	saves   map[platform.DialogTicket]Attachment // save panels of file cards being downloaded
	pasting bool

	lastTyping time.Time // the last POST /typing
	wake       time.Time // the typing expiry a frame is already asked for

	// The mention autocomplete: the "@query" from mentionAt to caret, and its rows.
	mentionAt, caret int
	dismissed        bool
	list             rexui.CompletionState
	items            []rexui.CompletionItem
	ids              []string

	menuMsg   *Message // the message whose context menu build runs for
	menuPerms int64
	menuBuild func(*[]ui.MenuItem)
	none      func(*base.Kit) // a composer accessory that draws nothing (the send button is ours)
}

// chatUpload is one file of the composer: on its way, uploaded (att) or refused (err).
type chatUpload struct {
	key  int
	name string
	size int64
	att  *Attachment
	err  string
}

const (
	chatMaxUpload   = 25 << 20 // the web's MAX_UPLOAD
	chatTypingEvery = 6 * time.Second
	chatPasteTag    = 0xd15c0d0c4a7 // RequestPaste's tag for the composer's pasted files
)

var chatStates = map[*Discordo]*chatX{}

// cx is the chat's state; it starts over when the chat changes.
func (d *Discordo) cx() *chatX {
	x := chatStates[d]
	if x == nil {
		x = &chatX{saves: map[platform.DialogTicket]Attachment{}, none: func(*base.Kit) {}}
		x.menuBuild = func(items *[]ui.MenuItem) { d.chatMenu(items) }
		chatStates[d] = x
	}

	if x.channel != d.channel {
		x.channel, x.uploads, x.editID, x.items, x.ids, x.lastTyping = d.channel, nil, "", x.items[:0], x.ids[:0], time.Time{}
	}

	return x
}

// chat is features/chat/channel-chat.tsx: the header, the transcript, the composer.
func (d *Discordo) chat(title, topic string, guild bool, perms int64, w, H float32) {
	k, c := d.k, d.k.Context()
	area := c.Parent()
	d.chatMakeEditor()

	d.begin("header", box{axis: ui.AxisX, w: ui.Pct(1), h: ui.Px(d.px(headerH)), l: 16, r: 16, gap: 8, cross: ui.CrossCenter})
	if guild {
		d.icon("hash", 20, greyple)
	}

	d.text(title, d.ui.s16b, snow)

	if topic != "" {
		d.begin("divider", box{l: 8, r: 8, h: ui.Px(d.px(24))})
		d.rule(line10, true)
		c.Pop()
		d.text(topic, d.ui.s12, greyple)
	}

	if dm := d.st.DM(d.channel); !guild && dm != nil {
		d.spacer()
		d.dmHeaderActions(dm)
	}

	c.Pop()
	d.rule(line5, false)

	k.NextSize(ui.Pct(1), ui.Grow(1))
	v := k.BeginScroll("transcript", &d.logScroll, base.ScrollVertical)
	d.transcript(v, title, guild, perms)
	k.CloseScrollContent(&v)
	rexui.ScrollBars(k.Kit, &v)
	k.CloseScroll(&v)

	d.typingIndicator()
	d.composerBar(title, guild, perms)

	if has(perms, permSend) && has(perms, permAttach) {
		d.chatDropZone(area, w, H)
	}
}

// chatDropZone is channel-chat.tsx's onDrop: files dropped anywhere on the chat are attached, under a "Solte para anexar" veil.
func (d *Discordo) chatDropZone(area ui.ID, w, H float32) {
	c := d.k.Context()
	c.AcceptDrop(area, ui.DropSpec{Kinds: ui.DropFiles})
	s := c.DropSignal(area)

	if s.Dropped {
		if drop := c.Drop(); !drop.Failed {
			d.chatAddPaths(drop.Paths(nil))
		}
	}

	if !s.Over {
		return
	}

	// absolute inset-2 rounded-xl border-2 border-dashed border-blurple bg-surface-content/90
	c.NextAnchor(ui.AnchorTopLeft, d.px(8), d.px(8))
	outline(c, blurple, d.px(2))
	b := box{axis: ui.AxisX, w: ui.Px(w - d.px(16)), h: ui.Px(H - d.px(16)), radius: 12, gap: 8, bg: alpha(surfaceContent, 0.9), cross: ui.CrossCenter, justify: ui.JustifyCenter}
	c.Push(c.Box(d.style(b)|ui.DrawBorder, "drop veil"))
	d.icon("paperclip", 24, snow)
	d.text("Solte para anexar", d.ui.s16b, snow)
	c.Pop()
}

// transcript is features/chat/transcript.tsx: the start of the channel (or a spinner), the days, the messages.
func (d *Discordo) transcript(v base.ScrollView, title string, guild bool, perms int64) {
	c := d.k.Context()
	log := d.st.Logs[d.channel]

	_, sy := c.Scroll(v.Inner)
	content, view := c.ScrollExtent(v.Inner)
	d.atEnd = float32(sy) >= content[1]-view[1]-d.px(80)

	// Near the top: the page before (NEAR * 3 in the web).
	if log != nil && !log.Complete && sy < float64(d.px(240)) {
		d.loadPage(d.channel)
	}

	// Older messages arrived above: keep what was in view where it was.
	first := ""
	if log != nil && len(log.List) > 0 {
		first = log.List[0].ID
	}

	switch {
	case first != d.ui.first:
		d.ui.adjust = d.ui.first != "" && d.pin == 0
		d.ui.first, d.ui.baseH = first, content[1]
		c.RequestFrame()
	case d.ui.adjust && content[1] != d.ui.baseH:
		c.SetScroll(v.Inner, 0, sy+float64(content[1]-d.ui.baseH))
		d.ui.adjust = false
	}

	if d.pin > 0 {
		c.SetScroll(v.Inner, 0, 1e9) // the layout clamps it to the end
		d.pin--
		c.RequestFrame()
	}

	// min-h-full justify-end: a short channel sits at the bottom.
	c.NextMinSize(0, view[1])
	// The messages run edge to edge (li -mx-4 px-4) so their hover and mention colours do; the rest is inset.
	d.begin("log", box{axis: ui.AxisY, w: ui.Pct(1), t: 16, b: 20, justify: ui.JustifyEnd})

	if log == nil || !log.Complete {
		d.begin("loading", box{axis: ui.AxisY, w: ui.Pct(1), t: 24, b: 24, l: 16, r: 16, cross: ui.CrossCenter})
		rexui.Spinner(d.k.Kit, "Carregando", rexui.SpinnerOpts{})
		c.Pop()
	} else {
		d.welcome(title, guild)
	}

	if log != nil {
		var colors map[string]int
		if g := d.st.Guild(d.guild); g != nil {
			colors = g.RoleColors()
		}

		for i := range log.List {
			m := &log.List[i]
			var prev *Message
			if i > 0 {
				prev = &log.List[i-1]
			}

			if prev == nil || !sameDay(prev.CreatedAt, m.CreatedAt) {
				d.daySeparator(m)
			}

			d.messageItem(m, prev, colors, perms)
		}
	}

	c.Pop()
}

// welcome is the transcript's first item once the whole channel is loaded.
func (d *Discordo) welcome(title string, guild bool) {
	c := d.k.Context()
	d.begin("welcome", box{axis: ui.AxisY, w: ui.Pct(1), t: 32, b: 32, l: 16, r: 16})

	d.begin("disc", box{w: ui.Px(d.px(64)), h: ui.Px(d.px(64)), radius: 32, bg: surfaceRaised, cross: ui.CrossCenter, justify: ui.JustifyCenter})
	if guild {
		d.icon("hash", 32, fog)
	} else {
		d.icon("message-circle", 32, fog)
	}
	c.Pop()

	d.begin("words", box{axis: ui.AxisY, t: 16, gap: 8, w: ui.Pct(1)})
	if guild {
		d.para("Boas-vindas a #"+title+"!", d.ui.s30b, snow)
		d.para("Este é o começo do canal #"+title+".", d.ui.s16, fog)
	} else {
		d.para(title, d.ui.s30b, snow)
		d.para("Este é o começo da sua conversa com "+title+".", d.ui.s16, fog)
	}
	c.Pop()

	c.Pop()
}

func sameDay(a, b time.Time) bool {
	a, b = a.Local(), b.Local()
	return a.YearDay() == b.YearDay() && a.Year() == b.Year()
}

// daySeparator: a line, the day, a line.
func (d *Discordo) daySeparator(m *Message) {
	c := d.k.Context()
	d.beginKey(ui.HashString(c.ParentKey(), "day "+m.ID), box{axis: ui.AxisX, w: ui.Pct(1), t: 16, b: 16, l: 16, r: 16, gap: 8, cross: ui.CrossCenter})

	c.NextSize(ui.Grow(1), ui.Px(1))
	c.NextBG(line10)
	c.Box(ui.DrawBackground, "left")

	d.text(Day(m.CreatedAt), d.ui.date, greyple)

	c.NextSize(ui.Grow(1), ui.Px(1))
	c.NextBG(line10)
	c.Box(ui.DrawBackground, "right")

	c.Pop()
}

// grouped: a message right after another of the same author within minutes (startsNewGroup).
func grouped(m, prev *Message) bool {
	return prev != nil && prev.Author.ID == m.Author.ID && m.CreatedAt.Sub(prev.CreatedAt) < 7*time.Minute && sameDay(prev.CreatedAt, m.CreatedAt)
}

var emojis = [...]string{"👍", "❤️", "😂", "😮", "😢", "🎉", "🔥", "👀"}

// messageItem is features/chat/message-item.tsx.
func (d *Discordo) messageItem(m *Message, prev *Message, colors map[string]int, perms int64) {
	k, c := d.k, d.k.Context()
	cx := d.cx()
	me := d.st.Me.ID
	group := grouped(m, prev)
	highlighted := m.MentionEveryone || slices.Contains(m.Mentions, me)
	editing := cx.editID == m.ID

	key := ui.HashString(c.ParentKey(), m.ID)
	hot := k.VisualOf(key, base.HoverOn).Hot > 0.5 || d.ui.picker == m.ID

	b := box{axis: ui.AxisX, w: ui.Pct(1), l: 16, r: 16, t: 2, b: 2, gap: 16}
	if !group {
		b.t = 6 // mt-4 pt-1: the margin is a gap of its own, outside the hover colour
		c.NextSize(ui.Pct(1), ui.Px(d.px(16)))
		c.BoxKeyed(0, ui.HashString(c.ParentKey(), "gap "+m.ID), nil)
	}

	switch {
	case highlighted && hot:
		b.bg = alpha(ember, 0.15)
	case highlighted:
		b.bg = alpha(ember, 0.10)
	case hot:
		b.bg = 0x0000001a // hover:bg-black/10
	}

	// Clickable for the right-click of the context menu (the web's long-press sheet).
	flags := d.style(b) | ui.Hoverable | ui.HotAnimation | ui.Clickable
	id := c.BoxKeyed(flags, key, nil)
	c.Push(id)

	if highlighted { // border-l-2 border-ember-orange
		c.NextAnchor(ui.AnchorLeft, 0, 0)
		c.NextSize(ui.Px(d.px(2)), ui.Pct(1))
		c.NextBG(ember)
		c.Box(ui.DrawBackground, "mark")
	}

	name := m.Author.Name()
	if g := d.st.Guild(d.guild); g != nil {
		if i := slices.IndexFunc(g.Members, func(x Member) bool { return x.User.ID == m.Author.ID }); i >= 0 {
			name = g.Members[i].Name()
		}
	}

	if group {
		d.begin("time", box{axis: ui.AxisX, w: ui.Px(d.px(40)), t: 4, justify: ui.JustifyEnd})
		if hot {
			d.text(m.Time, d.ui.time, greyple)
		}
		c.Pop()
	} else {
		// The avatar and the name open the author's profile.
		d.press(ui.HashString(c.ParentKey(), "avatar"), box{}, 0, name)
		d.avatar(m.Author, name, 40, rexui.PresenceNone, surfaceContent)
		if d.pop() {
			d.openProfile(m.Author.ID)
		}
	}

	d.begin("body", box{axis: ui.AxisY, w: ui.Grow(1), gap: 4})

	if !group {
		d.begin("head", box{axis: ui.AxisX, gap: 8, cross: ui.CrossCenter})
		color := uint32(snow)
		if rgb, ok := colors[m.Author.ID]; ok {
			color = uint32(rgb)<<8 | 0xff
		}

		d.press(ui.HashString(c.ParentKey(), "name"), box{}, 0, name)
		d.text(name, d.ui.s14s, color)
		if d.pop() {
			d.openProfile(m.Author.ID)
		}

		d.text(m.When, d.ui.time, greyple)
		c.Pop()
	}

	if editing {
		d.chatEditor(m)
	} else {
		if len(m.Body) > 0 {
			d.messageText(m)
		}

		for i := range m.Attachments {
			d.attachment(&m.Attachments[i])
		}

		if len(m.Reactions) > 0 {
			c.BeginFlow("reactions", ui.FlowOpts{GapX: d.px(4), GapY: d.px(4)})
			for _, r := range m.Reactions {
				d.reaction(m, r, perms)
			}
			c.EndFlow()
		}
	}

	c.Pop()

	// The hover bar, as the web's: react, edit (mine), delete.
	own := m.Author.ID == me
	canReact, canDelete := has(perms, permReact), own || has(perms, permManageMessages)
	if hot && !editing && (canReact || own || canDelete) {
		c.NextAnchor(ui.AnchorTopRight, d.px(16), -d.px(12))
		outline(c, line10, 1)
		bar := box{axis: ui.AxisX, bg: surfaceSidebar, radius: 6, l: 2, r: 2, t: 2, b: 2}
		c.Push(c.Box(d.style(bar)|ui.DrawBorder, "actions"))

		if canReact {
			sig := d.chatBarButton("smile-plus", "Adicionar reação", false)
			if item, ok := rexui.DropdownMenu(k.Kit, sig, d.emojiMenu); ok {
				d.toggleReaction(m, emojis[item])
			}

			d.ui.picker = ""
			if k.Popup().OpenBy(base.PopupItems, c.KeyOf(sig)) {
				d.ui.picker = m.ID
			}
		}

		if own && d.clicked(d.chatBarButton("pencil", "Editar mensagem", false)) {
			d.chatStartEdit(m)
		}

		if canDelete && d.clicked(d.chatBarButton("trash-2", "Excluir mensagem", true)) {
			d.ui.deleting, d.ui.confirm = m, true
		}

		c.Pop()
	}

	c.Pop()

	// The right-click menu: the actions of the web's mobile sheet.
	cx.menuMsg, cx.menuPerms = m, perms
	if item, ok := rexui.ContextMenu(k.Kit, id, cx.menuBuild); ok {
		d.chatMenuChose(m, perms, item)
	}
}

// Context menu items: 0 to 7 are the emojis.
const (
	chatMenuEdit = 100 + iota
	chatMenuCopy
	chatMenuDelete
)

// chatMenu builds the message's context menu (message-item.tsx's Sheet): react, copy, edit, delete.
func (d *Discordo) chatMenu(items *[]ui.MenuItem) {
	m, perms := d.cx().menuMsg, d.cx().menuPerms
	own := m.Author.ID == d.st.Me.ID

	if has(perms, permReact) {
		sub := make([]ui.MenuItem, len(emojis))
		for i, e := range emojis {
			sub[i] = ui.MenuItem{ID: i, Title: e}
		}

		*items = append(*items, ui.MenuItem{Title: "Reagir", Icon: "smile-plus", Symbol: "face.smiling", Sub: sub})
	}

	if m.Content != "" {
		*items = append(*items, ui.MenuItem{ID: chatMenuCopy, Title: "Copiar texto", Icon: "copy", Symbol: "doc.on.doc"})
	}

	if own {
		*items = append(*items, ui.MenuItem{ID: chatMenuEdit, Title: "Editar", Icon: "pencil", Symbol: "pencil"})
	}

	if own || has(perms, permManageMessages) {
		*items = append(*items, ui.MenuSeparator, ui.MenuItem{ID: chatMenuDelete, Title: "Excluir", Icon: "trash-2", Symbol: "trash", Destructive: true})
	}
}

func (d *Discordo) chatMenuChose(m *Message, perms int64, item int) {
	switch {
	case item >= 0 && item < len(emojis):
		d.toggleReaction(m, emojis[item])
	case item == chatMenuCopy:
		platform.SetClipboardText([]byte(m.Content))
	case item == chatMenuEdit:
		d.chatStartEdit(m)
	case item == chatMenuDelete:
		d.ui.deleting, d.ui.confirm = m, true
	}
}

// chatBarButton is the hover bar's ghost icon-xs; the delete one turns red on hover (hover:text-destructive).
func (d *Discordo) chatBarButton(name, label string, danger bool) ui.ID {
	c := d.k.Context()
	hot := d.press(ui.HashString(c.ParentKey(), label), box{w: ui.Px(d.px(24)), h: ui.Px(d.px(24)), radius: 4, cross: ui.CrossCenter, justify: ui.JustifyCenter}, surfaceHover, label)

	color := uint32(fog)
	switch {
	case hot && danger:
		color = red
	case hot:
		color = snow
	}

	d.icon(name, 14, color)
	id := c.Parent()
	c.Pop()
	rexui.Tooltip(d.k.Kit, id, label)

	return id
}

// chatMakeEditor makes the edit composer once, at a safe point (a hosted editor cannot be made inside a frame).
func (d *Discordo) chatMakeEditor() {
	cx := d.cx()
	if cx.edit != nil || cx.making || d.a == nil {
		return
	}

	cx.making = true
	d.a.AtSafePoint(func() {
		cx.edit, _ = rexui.NewComposer(d.a.Win, rexui.ComposerSetup{MaxLen: 4000, Placeholder: "Editar mensagem"})
	})
}

func (d *Discordo) chatStartEdit(m *Message) {
	cx := d.cx()
	if cx.edit == nil {
		cx.edit, _ = rexui.NewComposer(nil, rexui.ComposerSetup{MaxLen: 4000}) // not made yet: a drawn one
	}

	cx.editID, cx.editOrig, cx.editErr, cx.editFocus = m.ID, m.Content, "", true
	cx.edit.SetText([]byte(m.Content))
}

// chatEditor is message-item.tsx's Editor: Enter saves, Shift+Enter breaks the line, Esc cancels.
func (d *Discordo) chatEditor(m *Message) {
	k, c := d.k, d.k.Context()
	cx := d.cx()
	d.begin("editor", box{axis: ui.AxisY, w: ui.Pct(1), t: 4, b: 4, gap: 4})

	sig := rexui.Composer(k.Kit, "Editar mensagem", cx.edit, rexui.ComposerOpts{Accessory: cx.none})
	if cx.editFocus {
		cx.edit.Focus(k.Kit)
		cx.editFocus = false
	}

	switch {
	case sig.Cancelled:
		cx.editID = ""
	case sig.SendKey:
		d.chatSaveEdit(m)
	}

	d.begin("hint", box{axis: ui.AxisX, gap: 8})
	d.text("Esc para cancelar · Enter para salvar", d.ui.time, greyple)
	if cx.editErr != "" {
		d.text(cx.editErr, d.ui.time, red)
	}
	c.Pop()

	c.Pop()
}

// chatSaveEdit sends the edit (PATCH); nothing changed or nothing left only closes the editor, as the web's.
func (d *Discordo) chatSaveEdit(m *Message) {
	cx := d.cx()
	content := strings.TrimSpace(string(cx.edit.Text()))
	if content == "" || content == cx.editOrig {
		cx.editID = ""
		return
	}

	client, gen, channelID, id := d.client, d.gen, m.ChannelID, m.ID

	go func() {
		var out Message
		err := client.do(context.Background(), "PATCH", "/channels/"+channelID+"/messages/"+id, map[string]string{"content": content}, &out)

		d.post(func(d *Discordo) {
			cx := d.cx()
			if d.gen != gen {
				return
			}

			if err != nil {
				if cx.editID == id {
					cx.editErr = err.Error()
				}

				return
			}

			if d.st.Logs[channelID] != nil {
				d.st.AddMessages(channelID, false, out)
			}

			if cx.editID == id {
				cx.editID = ""
			}
		})
	}()
}

func (d *Discordo) emojiMenu(items *[]ui.MenuItem) {
	for i, e := range emojis {
		*items = append(*items, ui.MenuItem{ID: i, Title: e})
	}
}

// messageText is the message's text: 15 px on 24, mentions as chips.
func (d *Discordo) messageText(m *Message) {
	c := d.k.Context()
	spans := d.ui.spans[:0]

	for i, s := range m.Spans {
		// A mention is a chip that opens the profile (MentionChip); its tag is its place among the mentions.
		span := ui.Span{Off: uint32(s.Off), Len: uint32(s.Len), Style: 1, Flags: ui.SpanMention, Tag: uint32(i + 1)}
		if s.Edited {
			span = ui.Span{Off: uint32(s.Off), Len: uint32(s.Len), Style: 2}
		}

		spans = append(spans, span)
	}

	d.ui.spans = spans
	c.NextFont(d.ui.s15.key)
	c.NextTextColor(fog)
	c.NextSize(ui.Pct(1), ui.TextSize(0))
	sig := c.RichText(ui.HashString(c.ParentKey(), "text"), m.Body, spans, &d.ui.rich, ui.RichOpts{LineHeight: d.px(24)})

	if sig.Clicked == 0 {
		return
	}

	// The spans follow the mentions of the content in order (State.render).
	all := mention.FindAllStringSubmatch(m.Content, -1)
	if n := int(sig.Clicked) - 1; n < len(all) {
		if _, known := d.st.User(all[n][1]); known {
			d.openProfile(all[n][1])
		}
	}
}

// attachment is AttachmentView: an image up to 320 pt high, or a file card.
func (d *Discordo) attachment(a *Attachment) {
	c := d.k.Context()
	path := "/attachments/" + a.ID

	if strings.HasPrefix(a.ContentType, "image/") {
		maxW, maxH := d.px(520), d.px(320)
		h, st := d.image(path, int32(maxW), int32(maxH), false)

		w, ht := d.px(320), d.px(180) // until it decodes
		if info, ok := d.imgs.Info(h); ok && st == img.Ready {
			w, ht = float32(info.W), float32(info.H)
		}

		c.NextSize(ui.Px(w), ui.Px(ht))
		d.k.Image(img.HashString(a.ID), h, ui.FitContain, d.px(8), a.Filename)

		return
	}

	// Any other file (a video too: there is no inline player) is a card that downloads it.
	d.press(ui.HashString(c.ParentKey(), a.ID), box{axis: ui.AxisX, bg: surfaceRaised, radius: 8, l: 12, r: 12, t: 12, b: 12, gap: 12, cross: ui.CrossCenter}, surfaceHover, a.Filename)
	d.icon("file-text", 32, blurpleSoft)
	d.begin("file", box{axis: ui.AxisY})
	d.text(a.Filename, d.ui.s14m, cerulean)
	d.text(formatSize(a.Size), d.ui.s12, greyple)
	c.Pop()

	if d.pop() {
		d.chatDownload(*a)
	}
}

// chatDownload asks where to save an attachment (the web's <a download>); chatDialogs fetches it once answered.
func (d *Discordo) chatDownload(a Attachment) {
	if d.a == nil {
		return
	}

	t, err := d.a.SavePanel(platform.SaveSpec{Name: a.Filename})
	if err != nil {
		d.status = "Não foi possível baixar o arquivo: " + err.Error()
		return
	}

	d.cx().saves[t] = a
}

// reaction is a reaction chip: blurple when it is mine.
func (d *Discordo) reaction(m *Message, r Reaction, perms int64) {
	c := d.k.Context()
	mine := slices.Contains(r.Users, d.st.Me.ID)

	b := box{axis: ui.AxisX, l: 6, r: 6, t: 2, b: 2, gap: 4, radius: 6, bg: surfaceRaised, cross: ui.CrossCenter}
	if mine {
		b.bg = alpha(blurple, 0.2)
		outline(c, blurple, 1)
	}

	d.press(ui.HashString(c.ParentKey(), r.Emoji), b, 0, r.Emoji)
	d.text(r.Emoji, d.ui.s14, snow)

	color := uint32(fog)
	if mine {
		color = snow
	}

	d.text(itoa(len(r.Users)), d.ui.s12s, color)

	if d.pop() && (perms&permReact != 0 || mine) {
		d.toggleReaction(m, r.Emoji)
	}
}

// composerBar is features/chat/composer.tsx: the files, the field with the paperclip and send, and the hint under it.
func (d *Discordo) composerBar(title string, guild bool, perms int64) {
	k, c := d.k, d.k.Context()
	cx := d.cx()
	d.begin("composer", box{axis: ui.AxisY, w: ui.Pct(1), l: 16, r: 16, b: 20, gap: 4})

	if perms&permSend == 0 {
		d.begin("locked", box{axis: ui.AxisX, w: ui.Pct(1), bg: surfaceRaised, radius: 8, l: 16, r: 16, t: 12, b: 12, gap: 8, cross: ui.CrossCenter})
		d.icon("lock", 16, greyple)
		d.text("Você não tem permissão para enviar mensagens neste canal.", d.ui.s14, greyple)
		c.Pop()
		c.Pop()

		return
	}

	placeholder := "Conversar com " + title
	if guild {
		placeholder = "Conversar em #" + title
	}

	if placeholder != d.ui.placeholder {
		d.ui.placeholder = placeholder
		d.composer.SetPlaceholder(placeholder)
	}

	canAttach := has(perms, permAttach)

	// rounded-lg bg-surface-raised: the files (border-b), then the paperclip, the text and send.
	d.begin("field", box{axis: ui.AxisY, w: ui.Pct(1), bg: surfaceRaised, radius: 8})
	if len(cx.uploads) > 0 {
		d.chatUploadChips()
		d.rule(line5, false)
	}

	d.begin("row", box{axis: ui.AxisX, w: ui.Pct(1), l: 8, r: 8, t: 4, b: 4, gap: 4, cross: ui.CrossEnd})
	if canAttach && d.ghost("paperclip", "Anexar arquivo", 16) {
		d.chatPickFiles()
	}

	opts := rexui.ComposerOpts{Sending: d.sending, Accessory: cx.none, CompletionLabel: "Mencionar"}
	if len(cx.items) > 0 && !cx.dismissed {
		opts.Completion, opts.Items = &cx.list, cx.items
	}

	d.begin("text", box{axis: ui.AxisY, w: ui.Grow(1)})
	sig := rexui.Composer(k.Kit, "Mensagem", d.composer, opts)
	c.Pop()

	if d.chatSendButton(d.chatCanSend()) || sig.SendKey {
		d.send()
	}

	c.Pop()
	c.Pop()

	d.chatComposerSignals(sig, canAttach)

	d.begin("hint", box{l: 4})
	if d.status != "" {
		d.text(d.status, d.ui.time, red)
	} else {
		d.text("Enter envia · Shift + Enter quebra a linha", d.ui.time, greyple)
	}
	c.Pop()

	c.Pop()
}

// chatSent starts the composer over after a send: no mention list, the typing clock reset.
func (d *Discordo) chatSent() {
	cx := d.cx()
	cx.items, cx.ids, cx.lastTyping = cx.items[:0], cx.ids[:0], time.Time{}
}

// chatClearUploads takes the sent files out of the composer (uploads.clear()), if it is still that chat's.
func (d *Discordo) chatClearUploads(channelID string, ids []string) {
	cx := d.cx()
	if cx.channel != channelID {
		return
	}

	cx.uploads = slices.DeleteFunc(cx.uploads, func(u chatUpload) bool { return u.att != nil && slices.Contains(ids, u.att.ID) })
}

// chatSendButton is the primary icon-sm submit button, faded while it cannot send.
func (d *Discordo) chatSendButton(enabled bool) bool {
	c := d.k.Context()
	if !enabled {
		c.NextOpacity(0.5)
	}

	d.press(ui.HashString(c.ParentKey(), "send"), box{w: ui.Px(d.px(32)), h: ui.Px(d.px(32)), radius: 8, bg: blurple, cross: ui.CrossCenter, justify: ui.JustifyCenter}, 0x4752c4ff, "Enviar mensagem")
	d.icon("send-horizontal", 16, white)

	return d.pop() && enabled
}

// chatCanSend is the web's canSubmit: text or a finished file, nothing uploading, nothing sending.
func (d *Discordo) chatCanSend() bool {
	ids, uploading := d.chatReady()
	return !d.sending && !uploading && (len(ids) > 0 || strings.TrimSpace(string(d.composer.Text())) != "")
}

// chatReady are the IDs of the uploaded files, and whether any is still on its way.
func (d *Discordo) chatReady() (ids []string, uploading bool) {
	for _, u := range d.cx().uploads {
		switch {
		case u.att != nil:
			ids = append(ids, u.att.ID)
		case u.err == "":
			uploading = true
		}
	}

	return ids, uploading
}

// chatComposerSignals handles what the composer reported: the mention list, typing, pasted files.
func (d *Discordo) chatComposerSignals(sig rexui.ComposerSignal, canAttach bool) {
	cx := d.cx()

	switch {
	case sig.Completion == rexui.ResultAccept:
		d.chatInsertMention(cx.list.Sel)
	case sig.Chosen >= 0:
		d.chatInsertMention(sig.Chosen)
	case sig.Completion == rexui.ResultCancel || sig.Dismissed:
		cx.dismissed = true
	}

	if sig.Changed || sig.Selected {
		d.chatMention(sig.Changed)
	}

	// POST /typing at most every 6 s while typing (TYPING_EVERY_MS).
	if sig.Changed && len(d.composer.Text()) > 0 && time.Since(cx.lastTyping) > chatTypingEvery {
		cx.lastTyping = time.Now()
		client, channelID := d.client, d.channel
		go client.do(context.Background(), "POST", "/channels/"+channelID+"/typing", nil, nil)
	}

	if canAttach && d.a != nil && (sig.PastedFiles || sig.PastedImage) {
		kinds := platform.WantFiles
		if sig.PastedImage {
			kinds = platform.WantImage
		}

		cx.pasting = true
		d.a.RequestPaste(chatPasteTag, kinds)
	}
}

// chatMention finds the "@query" before the caret and the people it matches (composer.tsx suggestions).
func (d *Discordo) chatMention(changed bool) {
	cx := d.cx()
	text := string(d.composer.Text())
	caret := min(d.composer.Selection().Range().Start, len(text))

	if changed {
		cx.dismissed = false
	}

	cx.items, cx.ids = cx.items[:0], cx.ids[:0]
	start, query, ok := chatMentionQuery(text, caret)
	if !ok {
		return
	}

	for _, p := range chatMatches(d.chatMentionable(), query) {
		cx.items = append(cx.items, rexui.CompletionItem{Title: p.name, Detail: "@" + p.user.Username})
		cx.ids = append(cx.ids, p.user.ID)
	}

	if changed || start != cx.mentionAt {
		cx.list.Reset(len(cx.items), true)
	}

	cx.mentionAt, cx.caret = start, caret
}

// chatInsertMention replaces the "@query" with "<@id> ".
func (d *Discordo) chatInsertMention(i int) {
	cx := d.cx()
	if i < 0 || i >= len(cx.ids) {
		return
	}

	ins := "<@" + cx.ids[i] + "> "
	end := cx.mentionAt + len(ins)
	d.composer.Replace(platform.TextRange{Start: cx.mentionAt, End: cx.caret}, []byte(ins), platform.TextRange{Start: end, End: end})
	cx.items, cx.ids = cx.items[:0], cx.ids[:0]
}

// chatPerson is someone "@" can mention here, by the name shown in this chat.
type chatPerson struct {
	user User
	name string
}

// chatMentionable is channel-chat.tsx's mentionable: the guild's members, or the DM's people and me, by name.
func (d *Discordo) chatMentionable() []chatPerson {
	var people []chatPerson

	if g := d.st.Guild(d.guild); g != nil {
		for _, m := range g.Members {
			people = append(people, chatPerson{m.User, m.Name()})
		}
	} else if dm := d.st.DM(d.channel); dm != nil {
		for _, u := range append(d.st.Others(*dm), d.st.Me) {
			people = append(people, chatPerson{u, u.Name()})
		}
	}

	slices.SortFunc(people, func(a, b chatPerson) int { return strings.Compare(strings.ToLower(a.name), strings.ToLower(b.name)) })

	return people
}

// chatMatches are up to 8 people whose name or username holds the query.
func chatMatches(people []chatPerson, query string) []chatPerson {
	q := strings.ToLower(query)
	var out []chatPerson

	for _, p := range people {
		if len(out) == 8 {
			break
		}

		if strings.Contains(strings.ToLower(p.name), q) || strings.Contains(strings.ToLower(p.user.Username), q) {
			out = append(out, p)
		}
	}

	return out
}

// chatMentionQuery is format.ts mentionQuery: the "@word" right before the caret (a byte offset), if any.
func chatMentionQuery(text string, caret int) (start int, query string, ok bool) {
	before := text[:caret]
	at := strings.LastIndexByte(before, '@')
	if at < 0 {
		return 0, "", false
	}

	// (?:^|\s)@([^\s@<>]*)$
	if r, _ := utf8.DecodeLastRuneInString(before[:at]); at > 0 && !unicode.IsSpace(r) {
		return 0, "", false
	}

	query = before[at+1:]
	if strings.ContainsFunc(query, func(r rune) bool { return unicode.IsSpace(r) || r == '<' || r == '>' }) {
		return 0, "", false
	}

	return at, query, true
}

// chatUploadChips are the files of the next message: a spinner or the file icon, the name, the size or the error, X.
func (d *Discordo) chatUploadChips() {
	k, c := d.k, d.k.Context()
	cx := d.cx()
	removed := -1

	c.NextPadding(d.px(8), d.px(8), d.px(8), d.px(8))
	c.BeginFlow("uploads", ui.FlowOpts{GapX: d.px(8), GapY: d.px(8), Cross: ui.CrossCenter})

	for _, u := range cx.uploads {
		// rounded-md bg-surface-secondary py-1 pl-2 pr-1 text-xs; ring-1 ring-destructive on an error
		b := box{axis: ui.AxisX, l: 8, r: 4, t: 4, b: 4, gap: 8, radius: 6, bg: surfaceSidebar, cross: ui.CrossCenter}
		flags := d.style(b)
		if u.err != "" {
			c.NextBorder(red, 1)
			flags |= ui.DrawBorder
		}

		c.NextMaxSize(d.px(224), 0) // max-w-56
		c.Push(c.BoxKeyed(flags, ui.KeyN(c.ParentKey(), uint64(u.key)), nil))

		if u.att != nil || u.err != "" {
			d.icon("file-text", 16, greyple)
		} else {
			rexui.Spinner(k.Kit, "Enviando", rexui.SpinnerOpts{Size: theme.SizeMini, Backdrop: rexui.OfRGBA(surfaceSidebar)})
		}

		d.begin("name", box{axis: ui.AxisY})
		d.text(u.name, d.ui.s12, snow)
		if u.err != "" {
			d.text(u.err, d.ui.s12, red)
		} else {
			d.text(formatSize(u.size), d.ui.s12, greyple)
		}
		c.Pop()

		if d.clicked(d.chatBarButton("x", "Remover "+u.name, false)) {
			removed = u.key
		}

		c.Pop()
	}

	c.EndFlow()

	if removed >= 0 {
		cx.uploads = slices.DeleteFunc(cx.uploads, func(u chatUpload) bool { return u.key == removed })
	}
}

// chatPickFiles opens the native open panel (the web's hidden <input type=file multiple>).
func (d *Discordo) chatPickFiles() {
	if d.a == nil || d.cx().panel != 0 {
		return
	}

	t, err := d.a.OpenPanel(platform.OpenSpec{Files: true, Multiple: true})
	if err != nil {
		d.status = "Não foi possível abrir os arquivos: " + err.Error()
		return
	}

	d.cx().panel = t
}

// chatPasted takes the files or the image a paste in the composer brought.
func (d *Discordo) chatPasted(p app.Paste) {
	if p.Tag != chatPasteTag || p.Failed {
		return
	}

	switch p.Kind {
	case platform.PasteFiles:
		d.chatAddPaths(platform.PasteFilePaths(p.Data))
	case platform.PasteImage:
		data := slices.Clone(p.Data) // the App reuses its buffer
		d.chatUpload("image.png", int64(len(data)), func() ([]byte, error) { return data, nil })
	}
}

// chatAddPaths attaches files from the disk (the panel, a drop, a paste).
func (d *Discordo) chatAddPaths(paths []string) {
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}

		d.chatUpload(filepath.Base(path), info.Size(), func() ([]byte, error) { return os.ReadFile(path) })
	}
}

// chatUpload is useUploads' add: POST /channels/{c}/attachments, refused over 25 MB.
func (d *Discordo) chatUpload(name string, size int64, read func() ([]byte, error)) {
	cx := d.cx()
	cx.nextKey++
	key := cx.nextKey

	if size > chatMaxUpload {
		cx.uploads = append(cx.uploads, chatUpload{key: key, name: name, size: size, err: "Maior que 25 MB"})
		return
	}

	cx.uploads = append(cx.uploads, chatUpload{key: key, name: name, size: size})
	client, gen, channelID := d.client, d.gen, d.channel

	go func() {
		var att Attachment
		data, err := read()
		if err == nil {
			err = client.Upload(context.Background(), "/channels/"+channelID+"/attachments", name, data, &att)
		}

		d.post(func(d *Discordo) {
			cx := d.cx()
			i := slices.IndexFunc(cx.uploads, func(u chatUpload) bool { return u.key == key })
			if d.gen != gen || cx.channel != channelID || i < 0 {
				return // removed, or another chat now
			}

			if err != nil {
				cx.uploads[i].err = err.Error()
			} else {
				cx.uploads[i].att = &att
			}
		})
	}()
}

// typingIndicator is features/chat/typing-indicator.tsx: who else is typing, under the transcript.
func (d *Discordo) typingIndicator() {
	c := d.k.Context()
	cx := d.cx()
	now := time.Now()

	var ids []string
	var next time.Time
	for id, until := range d.st.Typing[d.channel] {
		if id == d.st.Me.ID || !until.After(now) {
			continue
		}

		ids = append(ids, id)
		if next.IsZero() || until.Before(next) {
			next = until
		}
	}

	// Draw again when the next one expires (nothing else would wake the window).
	if !next.IsZero() && !next.Equal(cx.wake) {
		cx.wake = next
		time.AfterFunc(next.Sub(now)+50*time.Millisecond, func() { d.post(func(*Discordo) {}) })
	}

	slices.Sort(ids) // a steady order between frames

	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = d.chatName(id)
	}

	d.begin("typing", box{axis: ui.AxisX, w: ui.Pct(1), h: ui.Px(d.px(20)), l: 20, r: 20, cross: ui.CrossCenter, clip: true})
	if text := typingText(names); text != "" {
		d.text(text, d.ui.s12, fog)
	}
	c.Pop()
}

// chatName is the name someone has in this chat (the web's people map), "Alguém" for a stranger.
func (d *Discordo) chatName(userID string) string {
	if g := d.st.Guild(d.guild); g != nil {
		if i := slices.IndexFunc(g.Members, func(m Member) bool { return m.User.ID == userID }); i >= 0 {
			return g.Members[i].Name()
		}
	}

	if u, ok := d.st.User(userID); ok {
		return u.Name()
	}

	return "Alguém"
}

// typingText is TypingIndicator's sentence.
func typingText(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0] + " está digitando…"
	case 2:
		return names[0] + " e " + names[1] + " estão digitando…"
	}

	return "Várias pessoas estão digitando…"
}

// memberList is features/members/member-list.tsx: online, then offline, each by name.
func (d *Discordo) memberList(g *Guild, H float32) {
	k, c := d.k, d.k.Context()
	colors := g.RoleColors()

	members := slices.Clone(g.Members)
	slices.SortFunc(members, func(a, b Member) int { return strings.Compare(strings.ToLower(a.Name()), strings.ToLower(b.Name())) })

	k.NextSize(ui.Grow(1), ui.Pct(1))
	v := k.BeginScroll("member list", &d.memberScroll, base.ScrollVertical)
	d.begin("groups", box{axis: ui.AxisY, w: ui.Pct(1), l: 12, r: 12, t: 24, b: 12, gap: 24})

	for _, online := range [...]bool{true, false} {
		n := 0
		for _, m := range members {
			if m.Online == online {
				n++
			}
		}

		if n == 0 {
			continue
		}

		label := "OFFLINE — "
		if online {
			label = "ONLINE — "
		}

		d.begin(label, box{axis: ui.AxisY, w: ui.Pct(1), gap: 2})
		d.begin("label", box{l: 8, r: 8, b: 6})
		d.text(label+itoa(n), d.ui.caption, greyple)
		c.Pop()

		for _, m := range members {
			if m.Online != online {
				continue
			}

			if !online {
				c.NextOpacity(0.45)
			}

			hot := d.press(ui.HashString(c.ParentKey(), m.User.ID), box{axis: ui.AxisX, w: ui.Pct(1), l: 8, r: 8, t: 6, b: 6, gap: 12, radius: 6, cross: ui.CrossCenter}, surfaceHover, m.Name())
			presence := rexui.PresenceNone
			ring := uint32(surfaceSidebar)
			if online {
				presence = rexui.PresenceOnline
			}
			if hot {
				ring = surfaceHover
			}

			d.avatar(m.User, m.Name(), 32, presence, ring)

			color := uint32(greyple)
			if hot {
				color = fog
			}
			if rgb, ok := colors[m.User.ID]; ok {
				color = uint32(rgb)<<8 | 0xff
			}

			d.text(m.Name(), d.ui.s14m, color)
			if d.pop() {
				d.openProfile(m.User.ID)
			}
		}

		c.Pop()
	}

	c.Pop()
	k.CloseScrollContent(&v)
	rexui.ScrollBars(k.Kit, &v)
	k.CloseScroll(&v)
}

// deleteDialog is the ConfirmDialog of a message's delete.
func (d *Discordo) deleteDialog() {
	k := d.k
	dlg := k.BeginDialog("delete", "Excluir mensagem", &d.ui.confirm)

	if dlg.Visible {
		k.Paragraph("Tem certeza de que quer excluir esta mensagem? Não dá para desfazer.")
		dlg.BeginActions()
		dlg.Action("Cancelar", widget.ButtonDefault)

		if dlg.Action("Excluir", widget.ButtonPrimary) && d.ui.deleting != nil {
			m := d.ui.deleting
			call(d, "DELETE", "/channels/"+m.ChannelID+"/messages/"+m.ID, nil, func(*Discordo, struct{}) {})
		}

		dlg.EndActions()
	}

	k.EndDialog(&dlg)
}

// chatDialogs draws the chat's dialogs (deleting a message) and takes the answers of its panels and pastes.
func (d *Discordo) chatDialogs() {
	d.deleteDialog()

	if d.a == nil {
		return
	}

	cx := d.cx()

	if cx.panel != 0 {
		if r, ok := d.a.TakeDialogFor(cx.panel); ok {
			cx.panel = 0
			if r.Outcome == platform.DialogAnswered {
				d.chatAddPaths(r.Paths)
			}
		}
	}

	for t, a := range cx.saves {
		r, ok := d.a.TakeDialogFor(t)
		if !ok {
			continue
		}

		delete(cx.saves, t)
		if r.Outcome == platform.DialogAnswered && len(r.Paths) > 0 {
			d.chatSave(a, r.Paths[0])
		}
	}

	if cx.pasting {
		if p, ok := d.a.TakePaste(); ok {
			cx.pasting = false
			d.chatPasted(p)
		}
	}
}

// chatSave downloads an attachment to path.
func (d *Discordo) chatSave(a Attachment, path string) {
	client, gen := d.client, d.gen

	go func() {
		data, err := client.Fetch(context.Background(), "/attachments/"+a.ID)
		if err == nil {
			err = os.WriteFile(path, data, 0o644)
		}

		d.post(func(d *Discordo) {
			if d.gen == gen && err != nil {
				d.status = "Não foi possível baixar o arquivo: " + err.Error()
			}
		})
	}()
}
