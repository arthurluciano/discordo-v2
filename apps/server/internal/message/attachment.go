package message

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"discordo/server/internal/errs"
	"discordo/server/internal/id"
	"discordo/server/internal/permission"
)

const MaxUploadSize = 25 << 20

type Attachment struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

type AttachmentRecord struct {
	Attachment
	ChannelID  string
	UploaderID string
	Sent       bool // already part of a message
}

// Blobs stores file contents under server-generated names.
// shortcut: blobs of unsent uploads and of deleted channels/guilds are never removed; add a sweeper (or a bucket lifecycle rule) before storage usage matters.
type Blobs interface {
	Put(ctx context.Context, name string, r io.Reader) (size int64, err error)
	Open(ctx context.Context, name string) (io.ReadSeekCloser, error)
	Delete(ctx context.Context, name string) error
}

// Upload stores a file for a message the user is about to send in the channel.
func (s *Service) Upload(ctx context.Context, userID, channelID, filename string, r io.Reader) (Attachment, error) {
	if _, err := s.access(ctx, userID, channelID, permission.SendMessages|permission.AttachFiles); err != nil {
		return Attachment{}, err
	}
	// The type is sniffed from the bytes; what the client claims is not trusted.
	head := make([]byte, 512)
	n, err := io.ReadFull(r, head)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return Attachment{}, errs.Invalid("unreadable upload")
	}
	head = head[:n]
	a := Attachment{ID: id.New(), Filename: cleanFilename(filename), ContentType: http.DetectContentType(head)}

	body := io.LimitReader(io.MultiReader(bytes.NewReader(head), r), MaxUploadSize+1)
	if a.Size, err = s.blobs.Put(ctx, a.ID, body); err != nil {
		return Attachment{}, err
	}
	if a.Size == 0 || a.Size > MaxUploadSize {
		s.blobs.Delete(ctx, a.ID)
		return Attachment{}, errs.Invalid("file must have between 1 byte and 25 MB")
	}
	if err := s.store.CreateAttachment(ctx, AttachmentRecord{Attachment: a, ChannelID: channelID, UploaderID: userID}); err != nil {
		s.blobs.Delete(ctx, a.ID)
		return Attachment{}, err
	}
	return a, nil
}

// OpenAttachment is allowed for whoever can see the channel; unsent uploads only for their uploader.
func (s *Service) OpenAttachment(ctx context.Context, userID, attachmentID string) (Attachment, io.ReadSeekCloser, error) {
	rec, err := s.store.Attachment(ctx, attachmentID)
	if err != nil {
		return Attachment{}, nil, err
	}
	if _, err := s.access(ctx, userID, rec.ChannelID, 0); err != nil {
		return Attachment{}, nil, err
	}
	if !rec.Sent && rec.UploaderID != userID {
		return Attachment{}, nil, errs.ErrNotFound
	}
	f, err := s.blobs.Open(ctx, rec.ID)
	return rec.Attachment, f, err
}

func cleanFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '"' {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." || name == "/" {
		return "file"
	}
	if utf8.RuneCountInString(name) > 200 {
		name = string([]rune(name)[:200])
	}
	return name
}
