// Package blob stores file contents in an S3-compatible bucket (MinIO in development).
package blob

import (
	"context"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"discordo/server/internal/errs"
)

type Config struct {
	Endpoint  string // host:port
	AccessKey string
	SecretKey string
	Bucket    string
	Secure    bool // TLS
}

type S3 struct {
	client *minio.Client
	bucket string
}

// NewS3 connects and makes sure the bucket exists.
func NewS3(ctx context.Context, cfg Config) (*S3, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.Secure,
	})
	if err != nil {
		return nil, err
	}
	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, err
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, err
		}
	}
	return &S3{client, cfg.Bucket}, nil
}

func (s *S3) Put(ctx context.Context, name string, r io.Reader) (int64, error) {
	// Size is unknown while streaming; the small part size keeps the per-upload buffer at 5 MiB.
	info, err := s.client.PutObject(ctx, s.bucket, name, r, -1, minio.PutObjectOptions{PartSize: 5 << 20})
	return info.Size, err
}

func (s *S3) Open(ctx context.Context, name string) (io.ReadSeekCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, name, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	// GetObject is lazy; Stat surfaces a missing object now instead of on the first read.
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		if minio.ToErrorResponse(err).Code == minio.NoSuchKey {
			return nil, errs.ErrNotFound
		}
		return nil, err
	}
	return obj, nil
}

func (s *S3) Delete(ctx context.Context, name string) error {
	return s.client.RemoveObject(ctx, s.bucket, name, minio.RemoveObjectOptions{})
}
