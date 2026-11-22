package db_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/larueli/biblisann/internal/db"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
)

// postgresImage is pinned by digest-free major tag for tests; production images are pinned in deploy/.
const postgresImage = "postgres:18-alpine"

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test: needs Docker")
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()
	ctr, err := postgres.Run(ctx, postgresImage,
		postgres.WithDatabase("biblisann"), postgres.WithUsername("test"), postgres.WithPassword("test"),
		postgres.BasicWaitStrategies(),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2)),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	testcontainers.CleanupContainer(t, ctr)
	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestMigrateAndQueries(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)

	// Concurrent replicas starting together must all succeed (advisory lock).
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- db.Migrate(ctx, pool) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent migrate: %v", err)
		}
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("second migrate must be a no-op: %v", err)
	}

	q := dbsqlc.New(pool)
	if _, err := q.SetInstanceSetting(ctx, dbsqlc.SetInstanceSettingParams{Key: "providers", Value: []byte(`["bnf"]`)}); err != nil {
		t.Fatal(err)
	}
	got, err := q.GetInstanceSetting(ctx, "providers")
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Value) != `["bnf"]` {
		t.Errorf("value = %s", got.Value)
	}
	if _, err := q.SetInstanceSetting(ctx, dbsqlc.SetInstanceSettingParams{Key: "", Value: []byte(`1`)}); err == nil {
		t.Error("empty key must violate the check constraint")
	}
}

func TestOpenInvalidDSNDoesNotLeak(t *testing.T) {
	_, err := db.Open(context.Background(), "postgres://user:hunter2@%zz/db")
	if err == nil {
		t.Fatal("expected an error")
	}
	if s := err.Error(); len(s) == 0 || strings.Contains(s, "hunter2") {
		t.Errorf("error leaks the DSN: %v", err)
	}
}
