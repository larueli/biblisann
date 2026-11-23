package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Config configures an S3-compatible bucket (AWS S3, MinIO, Ceph, Garage, ...).
type S3Config struct {
	Endpoint  string // host:port, no scheme
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
	UseSSL    bool
	Prefix    string // optional key prefix inside the bucket
}

// S3 stores blobs in an S3-compatible bucket. The endpoint is operator configuration, not user
// input, so it is not routed through safehttp (a MinIO service usually has a private address).
type S3 struct {
	c      *minio.Client
	bucket string
	prefix string
}

// NewS3 connects and verifies the bucket exists.
func NewS3(ctx context.Context, cfg S3Config) (*S3, error) {
	c, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL, Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: s3 client: %w", err)
	}
	ok, err := c.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("storage: s3 bucket check: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("storage: s3 bucket %q does not exist", cfg.Bucket)
	}
	return &S3{c: c, bucket: cfg.Bucket, prefix: strings.Trim(cfg.Prefix, "/")}, nil
}

func (s *S3) key(key string) (string, error) {
	if !keyRe.MatchString(key) {
		return "", errors.New("storage: invalid key")
	}
	if s.prefix == "" {
		return key, nil
	}
	return s.prefix + "/" + key, nil
}

// Put implements Store.
func (s *S3) Put(ctx context.Context, key string, data []byte) error {
	k, err := s.key(key)
	if err != nil {
		return err
	}
	_, err = s.c.PutObject(ctx, s.bucket, k, bytesReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "image/jpeg", CacheControl: "private, max-age=31536000, immutable"})
	if err != nil {
		return fmt.Errorf("storage: s3 put: %w", err)
	}
	return nil
}

// Open implements Store.
func (s *S3) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	k, err := s.key(key)
	if err != nil {
		return nil, err
	}
	obj, err := s.c.GetObject(ctx, s.bucket, k, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("storage: s3 get: %w", err)
	}
	// GetObject is lazy: Stat reveals a missing key.
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: s3 stat: %w", err)
	}
	return obj, nil
}

// Delete implements Store.
func (s *S3) Delete(ctx context.Context, key string) error {
	k, err := s.key(key)
	if err != nil {
		return err
	}
	if err := s.c.RemoveObject(ctx, s.bucket, k, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("storage: s3 delete: %w", err)
	}
	return nil
}

// List returns every stored key (used by the migration command).
func (s *S3) List(ctx context.Context) ([]string, error) {
	var out []string
	prefix := ""
	if s.prefix != "" {
		prefix = s.prefix + "/"
	}
	for o := range s.c.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if o.Err != nil {
			return nil, fmt.Errorf("storage: s3 list: %w", o.Err)
		}
		k := strings.TrimPrefix(o.Key, prefix)
		if keyRe.MatchString(k) {
			out = append(out, k)
		}
	}
	return out, nil
}
