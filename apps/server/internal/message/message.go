// Package message is chat in any channel (guild or DM): messages, attachments, reactions and typing.
package message

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"discordo/server/internal/errs"
	"discordo/server/internal/event"
	"discordo/server/internal/id"
	"discordo/server/internal/permission"
	"discordo/server/internal/user"
)

const (
	maxLength      = 4000
	maxAttachments = 10
	maxMentions    = 50
)

type Message struct {
	ID              string       `json:"id"`
	ChannelID       string       `json:"channel_id"`
	Author          user.User    `json:"author"`
	Content         string       `json:"content"`
	CreatedAt       time.Time    `json:"created_at"`
	EditedAt        *time.Time   `json:"edited_at"`
	Mentions        []string     `json:"mentions"` // user IDs written as <@id> in Content
	MentionEveryone bool         `json:"mention_everyone"`
	Attachments     []Attachment `json:"attachments"`
	Reactions       []Reaction   `json:"reactions"`
}

type Reaction struct {
	Emoji string   `json:"emoji"`
	Users []string `json:"users"`
}

type Store interface {
	// CreateMessage also attaches the given uploads, which must be unsent uploads of the author
	// in the same channel (ErrInvalid otherwise), and returns the stored message.
	CreateMessage(ctx context.Context, m Message, attachmentIDs []string) (Message, error)
	Message(ctx context.Context, messageID string) (Message, error)
	// ListMessages returns newest first, older than before when it is set.
	ListMessages(ctx context.Context, channelID, before string, limit int) ([]Message, error)
	UpdateMessage(ctx context.Context, m Message) error
	// DeleteMessage returns the IDs of the attachments that went with it.
	DeleteMessage(ctx context.Context, messageID string) (attachmentIDs []string, err error)
	SetReaction(ctx context.Context, messageID, userID, emoji string, on bool) error

	CreateAttachment(ctx context.Context, a AttachmentRecord) error
	Attachment(ctx context.Context, attachmentID string) (AttachmentRecord, error)
}

// Access is what a user may do in a channel and how to reach its audience.
type Access struct {
	Perms      permission.Set
	GuildID    string   // set for guild channels
	Recipients []string // set for DMs
}

type Authorizer interface {
	// ChannelAccess fails with ErrNotFound when the user cannot see the channel.
	ChannelAccess(ctx context.Context, userID, channelID string) (Access, error)
}

type Service struct {
	store Store
	blobs Blobs
	authz Authorizer
	pub   event.Publisher
}

func NewService(store Store, blobs Blobs, authz Authorizer, pub event.Publisher) *Service {
	return &Service{store: store, blobs: blobs, authz: authz, pub: pub}
}

func (s *Service) access(ctx context.Context, userID, channelID string, need permission.Set) (Access, error) {
	a, err := s.authz.ChannelAccess(ctx, userID, channelID)
	if err == nil && !a.Perms.Has(need) {
		err = errs.ErrForbidden
	}
	return a, err
}

func (s *Service) publish(a Access, channelID, typ string, data any) {
	ev := event.Event{Kind: event.ToUsers, UserIDs: a.Recipients, Type: typ, Data: data}
	if a.GuildID != "" {
		ev = event.Event{Kind: event.ToChannel, GuildID: a.GuildID, ChannelID: channelID, Type: typ, Data: data}
	}
	s.pub.Publish(ev)
}

var mentionRE = regexp.MustCompile(`<@([0-9a-fA-F-]{36})>`)

// setContent validates content and derives the mention fields from it.
func setContent(m *Message, content string, perms permission.Set, hasAttachments bool) error {
	content = strings.TrimSpace(content)
	n := utf8.RuneCountInString(content)
	if n > maxLength || (n == 0 && !hasAttachments) {
		return errs.Invalid("content must have 1 to 4000 characters")
	}
	m.Content = content
	m.Mentions = []string{}
	for _, match := range mentionRE.FindAllStringSubmatch(content, -1) {
		if uid := strings.ToLower(match[1]); id.Valid(uid) && !slices.Contains(m.Mentions, uid) && len(m.Mentions) < maxMentions {
			m.Mentions = append(m.Mentions, uid)
		}
	}
	m.MentionEveryone = perms.Has(permission.MentionEveryone) && strings.Contains(content, "@everyone")
	return nil
}

func (s *Service) Send(ctx context.Context, userID, channelID, content string, attachmentIDs []string) (Message, error) {
	need := permission.SendMessages
	if len(attachmentIDs) > 0 {
		need |= permission.AttachFiles
	}
	a, err := s.access(ctx, userID, channelID, need)
	if err != nil {
		return Message{}, err
	}
	if len(attachmentIDs) > maxAttachments || slices.ContainsFunc(attachmentIDs, func(v string) bool { return !id.Valid(v) }) {
		return Message{}, errs.Invalid("attachments must be at most 10 upload IDs")
	}
	m := Message{ID: id.New(), ChannelID: channelID, Author: user.User{ID: userID}}
	if err := setContent(&m, content, a.Perms, len(attachmentIDs) > 0); err != nil {
		return Message{}, err
	}
	if m, err = s.store.CreateMessage(ctx, m, attachmentIDs); err != nil {
		return Message{}, err
	}
	s.publish(a, channelID, "MESSAGE_CREATE", m)
	return m, nil
}

func (s *Service) List(ctx context.Context, userID, channelID, before string, limit int) ([]Message, error) {
	if _, err := s.access(ctx, userID, channelID, 0); err != nil {
		return nil, err
	}
	if before != "" && !id.Valid(before) {
		return nil, errs.Invalid("before must be a message ID")
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	return s.store.ListMessages(ctx, channelID, before, limit)
}

// in returns a message only if it belongs to the channel the caller was authorized for.
func (s *Service) in(ctx context.Context, channelID, messageID string) (Message, error) {
	m, err := s.store.Message(ctx, messageID)
	if err == nil && m.ChannelID != channelID {
		err = errs.ErrNotFound
	}
	return m, err
}

func (s *Service) Edit(ctx context.Context, userID, channelID, messageID, content string) (Message, error) {
	a, err := s.access(ctx, userID, channelID, permission.SendMessages)
	if err != nil {
		return Message{}, err
	}
	m, err := s.in(ctx, channelID, messageID)
	if err != nil {
		return Message{}, err
	}
	if m.Author.ID != userID {
		return Message{}, errs.ErrForbidden
	}
	if err := setContent(&m, content, a.Perms, len(m.Attachments) > 0); err != nil {
		return Message{}, err
	}
	if err := s.store.UpdateMessage(ctx, m); err != nil {
		return Message{}, err
	}
	if m, err = s.store.Message(ctx, messageID); err != nil {
		return Message{}, err
	}
	s.publish(a, channelID, "MESSAGE_UPDATE", m)
	return m, nil
}

func (s *Service) Delete(ctx context.Context, userID, channelID, messageID string) error {
	a, err := s.access(ctx, userID, channelID, 0)
	if err != nil {
		return err
	}
	m, err := s.in(ctx, channelID, messageID)
	if err != nil {
		return err
	}
	if m.Author.ID != userID && !a.Perms.Has(permission.ManageMessages) {
		return errs.ErrForbidden
	}
	attachmentIDs, err := s.store.DeleteMessage(ctx, messageID)
	if err != nil {
		return err
	}
	for _, aid := range attachmentIDs {
		s.blobs.Delete(ctx, aid)
	}
	s.publish(a, channelID, "MESSAGE_DELETE", map[string]string{"id": messageID, "channel_id": channelID})
	return nil
}

func (s *Service) React(ctx context.Context, userID, channelID, messageID, emoji string, on bool) error {
	var need permission.Set
	if on {
		need = permission.AddReactions
	}
	a, err := s.access(ctx, userID, channelID, need)
	if err != nil {
		return err
	}
	// Unicode emoji only: short, and no plain text smuggled in as a "reaction".
	if emoji == "" || len(emoji) > 64 || strings.ContainsFunc(emoji, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || (r < utf8.RuneSelf && unicode.IsLetter(r))
	}) {
		return errs.Invalid("emoji must be a single emoji")
	}
	if _, err := s.in(ctx, channelID, messageID); err != nil {
		return err
	}
	if err := s.store.SetReaction(ctx, messageID, userID, emoji, on); err != nil {
		return err
	}
	typ := "REACTION_REMOVE"
	if on {
		typ = "REACTION_ADD"
	}
	s.publish(a, channelID, typ, map[string]string{"channel_id": channelID, "message_id": messageID, "user_id": userID, "emoji": emoji})
	return nil
}

func (s *Service) Typing(ctx context.Context, userID, channelID string) error {
	a, err := s.access(ctx, userID, channelID, permission.SendMessages)
	if err != nil {
		return err
	}
	s.publish(a, channelID, "TYPING_START", map[string]string{"channel_id": channelID, "user_id": userID})
	return nil
}
