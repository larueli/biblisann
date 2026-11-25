// Package dbtest provides a throwaway, fully migrated PostgreSQL database for tests.
//
// One container is started per test binary and every test gets its own database, so tests stay
// isolated and parallel-safe. Tests are skipped with -short or when Docker is unavailable.
package dbtest

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/larueli/biblisann/internal/db"
)

// defaultImage is the PostgreSQL the suite runs on; set BIBLISANN_TEST_PG_IMAGE to try another one
// (CI runs the suite on every supported major version).
const defaultImage = "postgres:18-alpine"

func image() string {
	if v := os.Getenv("BIBLISANN_TEST_PG_IMAGE"); v != "" {
		return v
	}
	return defaultImage
}

var (
	once    sync.Once
	admin   *pgxpool.Pool
	startEr error
	counter atomic.Int64
)

// start launches the container. Rootless Docker occasionally hands out a host port another test
// package just took ("address already in use"): that is transient, so it retries a few times.
func start(ctx context.Context) {
	var ctr *postgres.PostgresContainer
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		if ctr, err = run(ctx); err == nil {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
	}
	if err != nil {
		startEr = err
		return
	}
	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		startEr = err
		return
	}
	admin, startEr = pgxpool.New(ctx, dsn)
}

func run(ctx context.Context) (*postgres.PostgresContainer, error) {
	return postgres.Run(ctx, image(),
		postgres.WithDatabase("postgres"), postgres.WithUsername("test"), postgres.WithPassword("test"),
		// Small and fast: this host may have little RAM and no swap.
		testcontainers.WithCmdArgs("-c", "shared_buffers=16MB", "-c", "max_connections=50", "-c", "fsync=off", "-c", "work_mem=2MB"),
		testcontainers.WithHostConfigModifier(func(hc *container.HostConfig) { hc.Memory = 384 << 20; hc.MemorySwap = 384 << 20 }),
		postgres.BasicWaitStrategies())
}

// Pool returns a pool on a new migrated database.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test: needs Docker")
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()
	once.Do(func() { start(ctx) })
	if startEr != nil {
		t.Fatalf("start postgres: %v", startEr)
	}
	name := fmt.Sprintf("t%d", counter.Add(1))
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	cfg := admin.Config().ConnConfig.Copy()
	cfg.Database = name
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable", cfg.User, cfg.Password, cfg.Host, cfg.Port, name)
	pool, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}
