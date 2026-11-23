package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// TestS3 runs against any S3-compatible endpoint given by the environment (MinIO, Garage, AWS):
//
//	BIBLISANN_TEST_S3_ENDPOINT=localhost:9000 BIBLISANN_TEST_S3_BUCKET=test \
//	BIBLISANN_TEST_S3_ACCESS_KEY=... BIBLISANN_TEST_S3_SECRET_KEY=... go test ./internal/storage
//
// It is skipped otherwise: no S3 server image is stable enough to start from the test itself.
func TestS3(t *testing.T) {
	ep := os.Getenv("BIBLISANN_TEST_S3_ENDPOINT")
	if ep == "" {
		t.Skip("set BIBLISANN_TEST_S3_ENDPOINT to run the S3 integration test")
	}
	ctx := context.Background()
	s, err := NewS3(ctx, S3Config{Endpoint: ep, Bucket: os.Getenv("BIBLISANN_TEST_S3_BUCKET"),
		AccessKey: os.Getenv("BIBLISANN_TEST_S3_ACCESS_KEY"), SecretKey: os.Getenv("BIBLISANN_TEST_S3_SECRET_KEY"),
		UseSSL: os.Getenv("BIBLISANN_TEST_S3_SSL") == "1", Prefix: "biblisann-test"})
	if err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("cd", 32)
	if _, err := s.Open(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing key: %v", err)
	}
	if err := s.Put(ctx, key, []byte("blob")); err != nil {
		t.Fatal(err)
	}
	r, err := s.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	_ = r.Close()
	if string(b) != "blob" {
		t.Errorf("content %q", b)
	}
	keys, _ := s.List(ctx)
	if len(keys) != 1 || keys[0] != key {
		t.Errorf("list: %v", keys)
	}
	if err := s.Put(ctx, "../evil", []byte("x")); err == nil {
		t.Error("bad key accepted")
	}
	// local -> s3 migration
	local, _ := NewLocal(t.TempDir())
	_ = local.Put(ctx, strings.Repeat("ef", 32), []byte("from local"))
	if n, err := Copy(ctx, local, s); err != nil || n != 1 {
		t.Errorf("migrate: %d %v", n, err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
}
