package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestS3 runs against RustFS (an S3-compatible server) started in a memory-capped container, or
// against any endpoint given by the environment (MinIO, Garage, AWS):
//
//	BIBLISANN_TEST_S3_ENDPOINT=host:9000 BIBLISANN_TEST_S3_BUCKET=b BIBLISANN_TEST_S3_ACCESS_KEY=k \
//	BIBLISANN_TEST_S3_SECRET_KEY=s go test ./internal/storage
func TestS3(t *testing.T) {
	ctx := context.Background()
	cfg := S3Config{Endpoint: os.Getenv("BIBLISANN_TEST_S3_ENDPOINT"), Bucket: os.Getenv("BIBLISANN_TEST_S3_BUCKET"),
		AccessKey: os.Getenv("BIBLISANN_TEST_S3_ACCESS_KEY"), SecretKey: os.Getenv("BIBLISANN_TEST_S3_SECRET_KEY"),
		UseSSL: os.Getenv("BIBLISANN_TEST_S3_SSL") == "1", Prefix: "biblisann-test"}
	if cfg.Endpoint == "" {
		if testing.Short() {
			t.Skip("integration test: needs Docker")
		}
		testcontainers.SkipIfProviderIsNotHealthy(t)
		ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{Started: true, ContainerRequest: testcontainers.ContainerRequest{
			Image:        "rustfs/rustfs:1.0.6",
			ExposedPorts: []string{"9000/tcp"},
			Env:          map[string]string{"RUSTFS_ACCESS_KEY": "biblisann-test", "RUSTFS_SECRET_KEY": "biblisann-test-secret"},
			// Small and bounded: this host may have little RAM and no swap.
			HostConfigModifier: func(hc *container.HostConfig) { hc.Memory = 384 << 20; hc.MemorySwap = 384 << 20 },
			WaitingFor:         wait.ForListeningPort("9000/tcp").WithStartupTimeout(90 * time.Second),
		}})
		if err != nil {
			t.Fatal(err)
		}
		testcontainers.CleanupContainer(t, ctr)
		ep, err := ctr.PortEndpoint(ctx, "9000/tcp", "")
		if err != nil {
			t.Fatal(err)
		}
		cfg.Endpoint, cfg.Bucket, cfg.AccessKey, cfg.SecretKey = ep, "covers", "biblisann-test", "biblisann-test-secret"
		admin, err := minio.New(ep, &minio.Options{Creds: credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, "")})
		if err != nil {
			t.Fatal(err)
		}
		var mkErr error
		for range 30 { // the port opens before the object layer is ready
			if mkErr = admin.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{}); mkErr == nil {
				break
			}
			time.Sleep(time.Second)
		}
		if mkErr != nil {
			t.Fatal(mkErr)
		}
	}
	s, err := NewS3(ctx, cfg)
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
