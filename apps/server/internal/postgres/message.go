package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"discordo/server/internal/errs"
	"discordo/server/internal/message"
)

const messageSQL = `
	select m.id, m.channel_id, m.content, m.created_at, m.edited_at, m.mentions::text[], m.mention_everyone,
		u.id, u.username, u.display_name, u.avatar_url,
		coalesce((
			select json_agg(json_build_object('id', a.id, 'filename', a.filename, 'content_type', a.content_type, 'size', a.size) order by a.id)
			from attachments a where a.message_id = m.id), '[]'::json),
		coalesce((
			select json_agg(json_build_object('emoji', r.emoji, 'users', r.users))
			from (select emoji, json_agg(user_id) as users from reactions where message_id = m.id group by emoji) r), '[]'::json)
	from messages m join users u on u.id = m.author_id `

func scanMessage(row pgx.CollectableRow) (m message.Message, err error) {
	err = row.Scan(&m.ID, &m.ChannelID, &m.Content, &m.CreatedAt, &m.EditedAt, &m.Mentions, &m.MentionEveryone,
		&m.Author.ID, &m.Author.Username, &m.Author.DisplayName, &m.Author.AvatarURL, &m.Attachments, &m.Reactions)
	return
}

func (s *Store) CreateMessage(ctx context.Context, m message.Message, attachmentIDs []string) (message.Message, error) {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			insert into messages (id, channel_id, author_id, content, mentions, mention_everyone)
			values ($1, $2, $3, $4, $5::text[]::uuid[], $6)`,
			m.ID, m.ChannelID, m.Author.ID, m.Content, m.Mentions, m.MentionEveryone); err != nil {
			return err
		}
		if len(attachmentIDs) == 0 {
			return nil
		}
		tag, err := tx.Exec(ctx, `
			update attachments set message_id = $1
			where id = any($2::text[]::uuid[]) and uploader_id = $3 and channel_id = $4 and message_id is null`,
			m.ID, attachmentIDs, m.Author.ID, m.ChannelID)
		if err == nil && int(tag.RowsAffected()) != len(attachmentIDs) {
			err = errs.Invalid("attachments must be your own unsent uploads to this channel")
		}
		return err
	})
	if err != nil {
		return m, mapErr(err)
	}
	return s.Message(ctx, m.ID)
}

func (s *Store) Message(ctx context.Context, messageID string) (message.Message, error) {
	rows, _ := s.db.Query(ctx, messageSQL+`where m.id = $1`, messageID)
	m, err := pgx.CollectExactlyOneRow(rows, scanMessage)
	return m, mapErr(err)
}

func (s *Store) ListMessages(ctx context.Context, channelID, before string, limit int) ([]message.Message, error) {
	var cursor *string
	if before != "" {
		cursor = &before
	}
	rows, _ := s.db.Query(ctx, messageSQL+`
		where m.channel_id = $1 and ($2::uuid is null or m.id < $2)
		order by m.id desc limit $3`, channelID, cursor, limit)
	msgs, err := pgx.CollectRows(rows, scanMessage)
	return msgs, mapErr(err)
}

func (s *Store) UpdateMessage(ctx context.Context, m message.Message) error {
	return execOne(ctx, s.db, `
		update messages set content = $2, mentions = $3::text[]::uuid[], mention_everyone = $4, edited_at = now()
		where id = $1`, m.ID, m.Content, m.Mentions, m.MentionEveryone)
}

func (s *Store) DeleteMessage(ctx context.Context, messageID string) (attachmentIDs []string, err error) {
	err = s.db.QueryRow(ctx, `
		delete from messages where id = $1
		returning (select coalesce(array_agg(id::text), '{}') from attachments where message_id = $1)`,
		messageID).Scan(&attachmentIDs)
	return attachmentIDs, mapErr(err)
}

func (s *Store) SetReaction(ctx context.Context, messageID, userID, emoji string, on bool) error {
	if !on {
		return exec(ctx, s.db, `delete from reactions where message_id = $1 and user_id = $2 and emoji = $3`, messageID, userID, emoji)
	}
	return exec(ctx, s.db, `insert into reactions values ($1, $2, $3) on conflict do nothing`, messageID, userID, emoji)
}

func (s *Store) CreateAttachment(ctx context.Context, a message.AttachmentRecord) error {
	return exec(ctx, s.db, `
		insert into attachments (id, channel_id, uploader_id, filename, content_type, size) values ($1, $2, $3, $4, $5, $6)`,
		a.ID, a.ChannelID, a.UploaderID, a.Filename, a.ContentType, a.Size)
}

func (s *Store) Attachment(ctx context.Context, attachmentID string) (a message.AttachmentRecord, err error) {
	err = s.db.QueryRow(ctx, `
		select id, filename, content_type, size, channel_id, uploader_id, message_id is not null
		from attachments where id = $1`, attachmentID,
	).Scan(&a.ID, &a.Filename, &a.ContentType, &a.Size, &a.ChannelID, &a.UploaderID, &a.Sent)
	return a, mapErr(err)
}
