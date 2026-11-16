// Package image stores pictures people upload for avatars, server icons and banners.
// They are public by URL (the ID is random), like any avatar shown to other users.
package image

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"discordo/server/internal/errs"
	"discordo/server/internal/id"
)

const (
	MaxSize    = 8 << 20
	pathPrefix = "/images/"
	blobPrefix = "images/"
)

var allowed = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// Blobs stores file contents under server-generated names.
type Blobs interface {
	Put(ctx context.Context, name string, r io.Reader) (size int64, err error)
	Open(ctx context.Context, name string) (io.ReadSeekCloser, error)
	Delete(ctx context.Context, name string) error
}

type Service struct{ blobs Blobs }

func NewService(blobs Blobs) *Service { return &Service{blobs: blobs} }

// Upload stores a picture and returns the URL to put in an avatar_url, icon_url or banner_url.
// shortcut: replaced pictures are never deleted, and uploads are not rate limited; add both before storage or abuse matters.
func (s *Service) Upload(ctx context.Context, r io.Reader) (string, error) {
	head := make([]byte, 512)
	n, err := io.ReadFull(r, head)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", errs.Invalid("unreadable upload")
	}
	head = head[:n]
	if !allowed[http.DetectContentType(head)] { // sniffed from the bytes; the client's claim is not trusted
		return "", errs.Invalid("image must be PNG, JPEG, GIF or WebP")
	}
	name := id.New()
	size, err := s.blobs.Put(ctx, blobPrefix+name, io.LimitReader(io.MultiReader(bytes.NewReader(head), r), MaxSize+1))
	if err != nil {
		return "", err
	}
	if size > MaxSize {
		s.blobs.Delete(ctx, blobPrefix+name)
		return "", errs.Invalid("image must be at most 8 MB")
	}
	return pathPrefix + name, nil
}

// Open returns a stored picture and its content type.
func (s *Service) Open(ctx context.Context, imageID string) (io.ReadSeekCloser, string, error) {
	if !id.Valid(imageID) {
		return nil, "", errs.ErrNotFound
	}
	f, err := s.blobs.Open(ctx, blobPrefix+imageID)
	if err != nil {
		return nil, "", err
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return nil, "", err
	}
	return f, http.DetectContentType(head[:n]), nil
}

// Valid reports whether url can be stored as a picture: empty (none) or one of our uploads.
// Outside URLs are refused so that showing a profile never makes clients fetch from third parties.
func Valid(url string) bool {
	rest, ok := strings.CutPrefix(url, pathPrefix)
	return url == "" || (ok && id.Valid(rest))
}
