package integration_test

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/larueli/biblisann/internal/catalog"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/providers"
)

// laterKnown does not know any book until learn is set, like a provider that added it since.
type laterKnown struct{ learn atomic.Bool }

func (*laterKnown) Name() string { return "fake" }
func (p *laterKnown) Lookup(_ context.Context, isbn string) (*providers.Record, error) {
	if p.learn.Load() {
		return &providers.Record{Title: "Learned title", Authors: []string{"Someone"}, Language: "en"}, nil
	}
	return nil, nil
}

func TestBatchRefreshOfUnknownBooks(t *testing.T) {
	e := newEnv(t)
	_, uid := e.user(t, "a@example.org")
	p := &laterKnown{}
	cat := catalog.NewService(e.pool, catalog.Config{Providers: []providers.Provider{p}, DefaultProviders: []string{"fake"},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	ctx := context.Background()
	mk := func(isbn, title string) dbsqlc.Edition {
		ed, _, err := cat.Create(ctx, uid, catalog.EditionInput{ISBN: isbn, Title: title, Authors: []string{"A"}}, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		return ed
	}
	unknown := mk("9780306406157", "Hand entered")
	excluded := mk("9782070612758", "Opted out")
	mk("", "No ISBN at all")
	q := dbsqlc.New(e.pool)
	if _, err := q.SetEditionSyncExcluded(ctx, dbsqlc.SetEditionSyncExcludedParams{ID: excluded.ID, SyncExcluded: true}); err != nil {
		t.Fatal(err)
	}

	// providers do not know the book: recorded, data kept; the excluded book and the one without ISBN are skipped
	rep, err := cat.RefreshBatch(ctx, catalog.RefreshScope{}, 0)
	if err != nil || rep.Checked != 1 || rep.NotFound != 1 {
		t.Fatalf("first run: %+v %v", rep, err)
	}
	got, _ := cat.Get(ctx, unknown.ID)
	if got.SyncStatus != catalog.StatusNotFound || got.SyncedAt == nil || got.Title != "Hand entered" {
		t.Fatalf("after a miss: %+v", got)
	}

	// a week must pass before the next automatic attempt, but an on-demand run ignores the wait
	if rep, _ := cat.RefreshBatch(ctx, catalog.RefreshScope{}, 0); rep.Checked != 0 {
		t.Errorf("second run inside the waiting period checked %d", rep.Checked)
	}
	p.learn.Store(true)
	rep, err = cat.RefreshBatch(ctx, catalog.RefreshScope{IgnoreWait: true}, 0)
	if err != nil || rep.Updated != 1 {
		t.Fatalf("on-demand run after the provider learned the book: %+v %v", rep, err)
	}
	got, _ = cat.Get(ctx, unknown.ID)
	if got.Title != "Learned title" || got.SyncStatus != catalog.StatusUpdated {
		t.Errorf("after the provider learned it: %+v", got)
	}
	if opt, _ := cat.Get(ctx, excluded.ID); opt.Title != "Opted out" || opt.SyncStatus != "" {
		t.Errorf("the excluded book was touched: %+v", opt)
	}
}
