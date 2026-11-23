package gc_test

import (
	"context"
	"encoding/hex"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/catalog"
	"github.com/larueli/biblisann/internal/dbtest"
	"github.com/larueli/biblisann/internal/gc"
	"github.com/larueli/biblisann/internal/library"
	"github.com/larueli/biblisann/internal/storage"
)

func TestRun(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	covers, _ := storage.NewLocal(t.TempDir())
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a, _ := auth.NewService(pool, auth.RegistrationOpen)
	u, _ := a.Register(ctx, "a@example.org", "tulips in the garden", "A")
	uctx := authz.With(ctx, &authz.Principal{UserID: u.ID, Via: "session"})
	lib := library.NewService(pool)
	cat := catalog.NewService(pool, catalog.Config{Covers: covers, Log: log})
	l, _ := lib.CreateLibrary(uctx, "Home")

	keep, _, _ := cat.Create(ctx, u.ID, catalog.EditionInput{Title: "Kept", Authors: []string{"A"}}, "", nil)
	inst, _ := lib.AddInstance(uctx, l.ID, library.InstanceInput{EditionID: keep.ID}, "")
	unused, _, _ := cat.Create(ctx, u.ID, catalog.EditionInput{Title: "Nobody owns me", Authors: []string{"B"}}, "", nil)
	deleted, _, _ := cat.Create(ctx, u.ID, catalog.EditionInput{Title: "Deleted", Authors: []string{"C"}}, "", nil)
	di, _ := lib.AddInstance(uctx, l.ID, library.InstanceInput{EditionID: deleted.ID}, "")
	if err := lib.DeleteInstance(uctx, di.ID); err != nil {
		t.Fatal(err)
	}

	// covers: one used, one orphan; both stored as files
	used := cat.StoreCover(ctx, redPNG())
	if err := cat.AttachCover(ctx, keep.ID, *used); err != nil {
		t.Fatal(err)
	}
	orphan := cat.StoreCover(ctx, bluePNG())
	sha, _ := cat.CoverHash(ctx, *orphan)

	// nothing is old enough yet: only grace-free things may go
	r, err := gc.Run(ctx, pool, covers, gc.Defaults(), log)
	if err != nil {
		t.Fatal(err)
	}
	if r.Instances != 0 || r.Editions != 0 || r.Covers != 0 {
		t.Errorf("fresh data must survive: %+v", r)
	}
	// age everything past the retention windows
	for _, q := range []string{
		"UPDATE instances SET deleted_at = now() - interval '100 days' WHERE deleted_at IS NOT NULL",
		"UPDATE editions SET created_at = now() - interval '40 days'",
		"UPDATE covers SET created_at = now() - interval '2 days'",
		"UPDATE changes SET created_at = now() - interval '40 days'",
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	r, err = gc.Run(ctx, pool, covers, gc.Defaults(), log)
	if err != nil {
		t.Fatal(err)
	}
	if r.Instances != 1 || r.Editions != 2 || r.Works != 2 || r.Covers != 1 || r.CoverFiles != 2 {
		t.Errorf("report: %s", r)
	}
	if _, err := covers.Open(ctx, sha); err == nil {
		t.Error("orphan cover file survived")
	}
	if rc, err := covers.Open(ctx, mustHash(t, cat, ctx, *used)); err != nil {
		t.Errorf("used cover file was removed: %v", err)
	} else {
		_ = rc.Close()
	}
	var n int
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM editions WHERE id = ANY($1)", []uuid.UUID{unused.ID, deleted.ID}).Scan(&n)
	if n != 0 {
		t.Errorf("unused editions remain: %d", n)
	}
	if got, err := lib.GetInstance(uctx, inst.ID); err != nil || got.EditionID != keep.ID {
		t.Errorf("owned copy damaged: %v", err)
	}
	// superseded change-log rows: the deleted copy had upsert then delete: only the delete remains, and the
	// kept copy keeps its single change. A second run finds nothing to do.
	if r2, _ := gc.Run(ctx, pool, covers, gc.Defaults(), log); r2 != (gc.Report{}) {
		t.Errorf("second run must be a no-op: %s", r2)
	}

	// tombstones past retention are forgotten and clients behind the horizon are told to resync
	idx, _ := lib.SyncIndex(uctx, l.ID, 0, 100)
	cursor := idx.Cursor
	if _, err := pool.Exec(ctx, "UPDATE changes SET created_at = now() - interval '200 days'"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO changes (library_id, entity, entity_id, op, created_at) VALUES ($1, 'instance', $2, 'delete', now() - interval '200 days')", l.ID, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if _, err := gc.Run(ctx, pool, covers, gc.Defaults(), log); err != nil {
		t.Fatal(err)
	}
	stale, err := lib.SyncIndex(uctx, l.ID, 1, 100)
	if err != nil || !stale.Reset {
		t.Errorf("a cursor behind the horizon must reset: %+v %v", stale, err)
	}
	if fresh, _ := lib.SyncIndex(uctx, l.ID, cursor+1000, 100); fresh.Reset {
		t.Error("a current cursor must not reset")
	}
}

func mustHash(t *testing.T, c *catalog.Service, ctx context.Context, id uuid.UUID) string {
	h, err := c.CoverHash(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hex.DecodeString(h); err != nil {
		t.Fatal(err)
	}
	return h
}
