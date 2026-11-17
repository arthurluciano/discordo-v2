package main

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"time"
)

// The protocol's types (docs/protocol.md), the fields this app shows.

type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
}

func (u User) Name() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}

	return u.Username
}

const (
	ChannelText  = 0
	ChannelVoice = 1
	ChannelDM    = 2
	ChannelGroup = 3
)

type Channel struct {
	ID          string `json:"id"`
	Type        int    `json:"type"`
	Name        string `json:"name"`
	Topic       string `json:"topic"`
	Permissions int64  `json:"permissions"`
}

type Role struct {
	ID       string `json:"id"`
	Color    int    `json:"color"`
	Position int    `json:"position"`
}

type Member struct {
	User     User     `json:"user"`
	Nickname string   `json:"nickname"`
	Roles    []string `json:"roles"`
	Online   bool     `json:"online"`
}

func (m Member) Name() string {
	if m.Nickname != "" {
		return m.Nickname
	}

	return m.User.Name()
}

type Guild struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	IconURL     string    `json:"icon_url"`
	OwnerID     string    `json:"owner_id"`
	Permissions int64     `json:"permissions"`
	Roles       []Role    `json:"roles"`
	Channels    []Channel `json:"channels"`
	Members     []Member  `json:"members"`
}

type DM struct {
	ID         string `json:"id"`
	Type       int    `json:"type"`
	Name       string `json:"name"`
	Recipients []User `json:"recipients"`
}

type Relationship struct {
	User   User `json:"user"`
	Status int  `json:"status"` // 1 friend, 2 incoming, 3 outgoing, 4 blocked
	Online bool `json:"online"`
}

type VoiceState struct {
	UserID    string   `json:"user_id"`
	ChannelID string   `json:"channel_id"`
	Muted     bool     `json:"muted"`
	Deafened  bool     `json:"deafened"`
	Tracks    []string `json:"tracks"`
}

type Attachment struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

type Reaction struct {
	Emoji string   `json:"emoji"`
	Users []string `json:"users"`
}

type Message struct {
	ID              string       `json:"id"`
	ChannelID       string       `json:"channel_id"`
	Author          User         `json:"author"`
	Content         string       `json:"content"`
	CreatedAt       time.Time    `json:"created_at"`
	EditedAt        *time.Time   `json:"edited_at"`
	Mentions        []string     `json:"mentions"`
	MentionEveryone bool         `json:"mention_everyone"`
	Attachments     []Attachment `json:"attachments"`
	Reactions       []Reaction   `json:"reactions"`

	// Made once when the message arrives, so a frame builds no strings: the text with mentions by name
	// and where they are (Spans), the times as the web app writes them.
	Body       []byte
	Spans      []Span
	When, Time string
}

// Span is a mention (or the "(editada)" mark) in a message's Body.
type Span struct {
	Off, Len int
	Edited   bool
}

const (
	permSend   = 2
	permReact  = 16
	permManage = 4096 // manage channels
)

type Event struct {
	T string          `json:"t"`
	D json.RawMessage `json:"d"`
}

// Log is a channel's messages, oldest first. Complete: its first message is the channel's first.
type Log struct {
	List     []Message
	Complete bool
}

// State is what the gateway and the REST calls told us. It changes only on the UI thread.
type State struct {
	Me            User
	Guilds        []Guild
	DMs           []DM
	Relationships []Relationship
	Voice         map[string]VoiceState // by user

	Logs   map[string]*Log
	online map[string]bool // user → online, from members, friends and PRESENCE_UPDATE
	users  map[string]User // everyone we know, for mentions and DMs
}

func NewState() *State {
	return &State{Logs: map[string]*Log{}, Voice: map[string]VoiceState{}, online: map[string]bool{}, users: map[string]User{}}
}

func decode[T any](raw json.RawMessage) (T, error) {
	var v T
	err := json.Unmarshal(raw, &v)

	return v, err
}

// Apply takes one gateway event. Events may repeat: everything is applied by ID.
func (s *State) Apply(e Event) error {
	switch e.T {
	case "READY":
		d, err := decode[struct {
			User          User           `json:"user"`
			Guilds        []Guild        `json:"guilds"`
			DMs           []DM           `json:"dms"`
			Relationships []Relationship `json:"relationships"`
			VoiceStates   []VoiceState   `json:"voice_states"`
		}](e.D)
		if err != nil {
			return err
		}

		*s = *NewState()
		s.Me, s.Guilds, s.DMs, s.Relationships = d.User, d.Guilds, d.DMs, d.Relationships
		s.learn(d.User)
		s.online[d.User.ID] = true

		for _, g := range d.Guilds {
			s.learnGuild(g)
		}

		for _, dm := range d.DMs {
			s.learn(dm.Recipients...)
		}

		for _, r := range d.Relationships {
			s.learn(r.User)
			s.online[r.User.ID] = r.Online
		}

		for _, v := range d.VoiceStates {
			s.Voice[v.UserID] = v
		}

	case "GUILD_SYNC":
		g, err := decode[Guild](e.D)
		if err != nil {
			return err
		}

		s.learnGuild(g)
		s.Guilds = upsert(s.Guilds, g, func(x Guild) string { return x.ID })

	case "GUILD_DELETE":
		d, err := decode[struct{ ID string }](e.D)
		if err != nil {
			return err
		}

		s.Guilds = slices.DeleteFunc(s.Guilds, func(g Guild) bool { return g.ID == d.ID })

	case "MEMBER_ADD":
		d, err := decode[struct {
			GuildID string `json:"guild_id"`
			Member  Member `json:"member"`
		}](e.D)
		if err != nil {
			return err
		}

		if g := s.Guild(d.GuildID); g != nil {
			g.Members = upsert(g.Members, d.Member, func(m Member) string { return m.User.ID })
			s.learnGuild(*g)
		}

	case "MEMBER_REMOVE":
		d, err := decode[struct {
			GuildID string `json:"guild_id"`
			UserID  string `json:"user_id"`
		}](e.D)
		if err != nil {
			return err
		}

		if g := s.Guild(d.GuildID); g != nil {
			g.Members = slices.DeleteFunc(g.Members, func(m Member) bool { return m.User.ID == d.UserID })
		}

	case "PRESENCE_UPDATE":
		d, err := decode[struct {
			UserID string `json:"user_id"`
			Online bool   `json:"online"`
		}](e.D)
		if err != nil {
			return err
		}

		s.online[d.UserID] = d.Online

		for gi := range s.Guilds {
			for mi := range s.Guilds[gi].Members {
				if m := &s.Guilds[gi].Members[mi]; m.User.ID == d.UserID {
					m.Online = d.Online
				}
			}
		}

		for i := range s.Relationships {
			if r := &s.Relationships[i]; r.User.ID == d.UserID {
				r.Online = d.Online
			}
		}

	case "DM_CREATE", "DM_UPDATE":
		dm, err := decode[DM](e.D)
		if err != nil {
			return err
		}

		s.learn(dm.Recipients...)
		s.DMs = upsert(s.DMs, dm, func(x DM) string { return x.ID })

	case "DM_DELETE":
		d, err := decode[struct{ ID string }](e.D)
		if err != nil {
			return err
		}

		s.DMs = slices.DeleteFunc(s.DMs, func(dm DM) bool { return dm.ID == d.ID })

	case "RELATIONSHIP_UPDATE":
		r, err := decode[Relationship](e.D)
		if err != nil {
			return err
		}

		s.learn(r.User)
		s.online[r.User.ID] = r.Online

		if r.Status == 0 {
			s.Relationships = slices.DeleteFunc(s.Relationships, func(x Relationship) bool { return x.User.ID == r.User.ID })
		} else {
			s.Relationships = upsert(s.Relationships, r, func(x Relationship) string { return x.User.ID })
		}

	case "USER_UPDATE":
		u, err := decode[User](e.D)
		if err != nil {
			return err
		}

		s.learn(u)

		if u.ID == s.Me.ID {
			s.Me = u
		}

		for gi := range s.Guilds {
			for mi := range s.Guilds[gi].Members {
				if m := &s.Guilds[gi].Members[mi]; m.User.ID == u.ID {
					m.User = u
				}
			}
		}

		for i := range s.DMs {
			for j := range s.DMs[i].Recipients {
				if s.DMs[i].Recipients[j].ID == u.ID {
					s.DMs[i].Recipients[j] = u
				}
			}
		}

	case "VOICE_STATE_UPDATE":
		v, err := decode[VoiceState](e.D)
		if err != nil {
			return err
		}

		if v.ChannelID == "" {
			delete(s.Voice, v.UserID)
		} else {
			s.Voice[v.UserID] = v
		}

	case "MESSAGE_CREATE", "MESSAGE_UPDATE":
		m, err := decode[Message](e.D)
		if err != nil {
			return err
		}

		// A channel never opened has no log: it is loaded whole when opened.
		if s.Logs[m.ChannelID] != nil {
			s.AddMessages(m.ChannelID, false, m)
		}

	case "MESSAGE_DELETE":
		d, err := decode[struct {
			ID        string `json:"id"`
			ChannelID string `json:"channel_id"`
		}](e.D)
		if err != nil {
			return err
		}

		if log := s.Logs[d.ChannelID]; log != nil {
			log.List = slices.DeleteFunc(log.List, func(m Message) bool { return m.ID == d.ID })
		}

	case "REACTION_ADD", "REACTION_REMOVE":
		d, err := decode[struct {
			ChannelID string `json:"channel_id"`
			MessageID string `json:"message_id"`
			UserID    string `json:"user_id"`
			Emoji     string `json:"emoji"`
		}](e.D)
		if err != nil {
			return err
		}

		if m := s.message(d.ChannelID, d.MessageID); m != nil {
			m.Reactions = react(m.Reactions, d.Emoji, d.UserID, e.T == "REACTION_ADD")
		}
	}

	return nil
}

// react adds or removes a user's reaction, keeping the order reactions came in and dropping an empty one.
func react(list []Reaction, emoji, userID string, add bool) []Reaction {
	i := slices.IndexFunc(list, func(r Reaction) bool { return r.Emoji == emoji })
	if i < 0 {
		if !add {
			return list
		}

		return append(list, Reaction{Emoji: emoji, Users: []string{userID}})
	}

	users := slices.DeleteFunc(slices.Clone(list[i].Users), func(u string) bool { return u == userID })
	if add {
		users = append(users, userID)
	}

	if len(users) == 0 {
		return slices.Delete(list, i, i+1)
	}

	list[i].Users = users

	return list
}

func (s *State) message(channelID, id string) *Message {
	log := s.Logs[channelID]
	if log == nil {
		return nil
	}

	for i := range log.List {
		if log.List[i].ID == id {
			return &log.List[i]
		}
	}

	return nil
}

// maxKept bounds a channel's messages in memory: the oldest are dropped.
const maxKept = 500

// AddMessages merges messages into a channel's log by ID (a new copy replaces the old), in order.
// complete says the server has no older messages than these.
func (s *State) AddMessages(channelID string, complete bool, msgs ...Message) {
	log := s.Logs[channelID]
	if log == nil {
		log = &Log{}
		s.Logs[channelID] = log
	}

	log.Complete = log.Complete || complete

	for _, m := range msgs {
		s.learn(m.Author)
		s.render(&m)
		log.List = upsert(log.List, m, func(x Message) string { return x.ID })
	}

	// IDs are UUIDv7: their order is the order of creation.
	slices.SortFunc(log.List, func(a, b Message) int { return strings.Compare(a.ID, b.ID) })

	if len(log.List) > maxKept {
		log.List = log.List[len(log.List)-maxKept:]
		log.Complete = false
	}
}

var mention = regexp.MustCompile(`<@([^<>\s]+)>`)

var months = [...]string{"jan.", "fev.", "mar.", "abr.", "mai.", "jun.", "jul.", "ago.", "set.", "out.", "nov.", "dez."}

// render makes what a frame draws: mentions as "@name" spans, "(editada)", the times as the web writes them.
func (s *State) render(m *Message) {
	var body []byte
	var spans []Span

	rest := m.Content
	for _, at := range mention.FindAllStringSubmatchIndex(m.Content, -1) {
		start, end := at[0]-(len(m.Content)-len(rest)), at[1]-(len(m.Content)-len(rest))
		body = append(body, rest[:start]...)

		name := "usuário desconhecido"
		if u, ok := s.users[m.Content[at[2]:at[3]]]; ok {
			name = u.Name()
		}

		spans = append(spans, Span{Off: len(body), Len: len(name) + 1})
		body = append(append(body, '@'), name...)
		rest = rest[end:]
	}

	body = append(body, rest...)

	if m.EditedAt != nil && len(body) > 0 {
		body = append(body, ' ')
		spans = append(spans, Span{Off: len(body), Len: len("(editada)"), Edited: true})
		body = append(body, "(editada)"...)
	}

	t := m.CreatedAt.Local()
	m.Body, m.Spans = body, spans
	m.Time = t.Format("15:04")
	m.When = t.Format("02") + " de " + months[t.Month()-1] + " às " + m.Time
}

// Day is the day separator's text: "16 de novembro de 2026".
func Day(t time.Time) string {
	names := [...]string{"janeiro", "fevereiro", "março", "abril", "maio", "junho", "julho", "agosto", "setembro", "outubro", "novembro", "dezembro"}
	t = t.Local()

	return strings.TrimPrefix(t.Format("02"), "0") + " de " + names[t.Month()-1] + " de " + t.Format("2006")
}

func (s *State) Guild(id string) *Guild {
	for i := range s.Guilds {
		if s.Guilds[i].ID == id {
			return &s.Guilds[i]
		}
	}

	return nil
}

func (s *State) DM(id string) *DM {
	for i := range s.DMs {
		if s.DMs[i].ID == id {
			return &s.DMs[i]
		}
	}

	return nil
}

func (g *Guild) Channel(id string) *Channel {
	for i := range g.Channels {
		if g.Channels[i].ID == id {
			return &g.Channels[i]
		}
	}

	return nil
}

// RoleColors: each member's highest coloured role, as 0xRRGGBB (absent: no colour).
func (g *Guild) RoleColors() map[string]int {
	colored := slices.DeleteFunc(slices.Clone(g.Roles), func(r Role) bool { return r.Color == 0 })
	slices.SortFunc(colored, func(a, b Role) int { return b.Position - a.Position })

	colors := map[string]int{}
	for _, m := range g.Members {
		for _, r := range colored {
			if slices.Contains(m.Roles, r.ID) {
				colors[m.User.ID] = r.Color
				break
			}
		}
	}

	return colors
}

func (s *State) Online(userID string) bool { return s.online[userID] }

func (s *State) User(id string) (User, bool) {
	u, ok := s.users[id]
	return u, ok
}

// Others are a DM's recipients but me.
func (s *State) Others(dm DM) []User {
	return slices.DeleteFunc(slices.Clone(dm.Recipients), func(u User) bool { return u.ID == s.Me.ID })
}

func (s *State) DMName(dm DM) string {
	if dm.Name != "" {
		return dm.Name
	}

	var names []string
	for _, u := range s.Others(dm) {
		names = append(names, u.Name())
	}

	if len(names) == 0 {
		return "Só você"
	}

	return strings.Join(names, ", ")
}

// Callers are the voice states in a channel.
func (s *State) Callers(channelID string) []VoiceState {
	var list []VoiceState
	for _, v := range s.Voice {
		if v.ChannelID == channelID {
			list = append(list, v)
		}
	}

	slices.SortFunc(list, func(a, b VoiceState) int { return strings.Compare(a.UserID, b.UserID) })

	return list
}

func (s *State) learn(users ...User) {
	for _, u := range users {
		s.users[u.ID] = u
	}
}

func (s *State) learnGuild(g Guild) {
	for _, m := range g.Members {
		s.learn(m.User)
		s.online[m.User.ID] = m.Online
	}
}

func upsert[T any](list []T, v T, id func(T) string) []T {
	if i := slices.IndexFunc(list, func(x T) bool { return id(x) == id(v) }); i >= 0 {
		list[i] = v
		return list
	}

	return append(list, v)
}
