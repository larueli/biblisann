package catalog_test

import (
	"context"
	"testing"

	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/catalog"
	"github.com/larueli/biblisann/internal/dbtest"
	"github.com/larueli/biblisann/internal/library"
	"github.com/larueli/biblisann/internal/providers"
)

func TestRefreshHonorsAllHoldingLibraries(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	a, err := auth.NewService(pool, auth.RegistrationOpen)
	if err != nil {
		t.Fatal(err)
	}
	u, err := a.Register(ctx, "reader@example.org", "tulips in the garden", "Reader")
	if err != nil {
		t.Fatal(err)
	}
	ctx = authz.With(ctx, &authz.Principal{UserID: u.ID, IsAdmin: true, Via: "session"})
	first := &fakeProvider{name: "first"}
	second := &fakeProvider{name: "second"}
	c := catalog.NewService(pool, catalog.Config{Providers: []providers.Provider{first, second}})
	l := library.NewService(pool)
	c.SetAccess(l)
	ed, _, err := c.Create(ctx, u.ID, catalog.EditionInput{ISBN: "9782070612758", Title: "Book"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	refresh := func() {
		t.Helper()
		if _, _, err := c.Refresh(ctx, ed.ID); err != nil {
			t.Fatal(err)
		}
	}
	refresh()
	if first.calls.Load() != 1 || second.calls.Load() != 1 {
		t.Fatalf("unowned edition must use defaults: %d %d", first.calls.Load(), second.calls.Load())
	}
	lib1, err := l.CreateLibrary(ctx, "One")
	if err != nil {
		t.Fatal(err)
	}
	lib2, err := l.CreateLibrary(ctx, "Two")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = l.UpdateLibrary(ctx, lib1.ID, "One", []byte(`{"providers":["first","second"]}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = l.UpdateLibrary(ctx, lib2.ID, "Two", []byte(`{"providers":["first"]}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = l.AddInstance(ctx, lib1.ID, library.InstanceInput{EditionID: ed.ID}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = l.AddInstance(ctx, lib2.ID, library.InstanceInput{EditionID: ed.ID}, ""); err != nil {
		t.Fatal(err)
	}
	refresh()
	if first.calls.Load() != 2 || second.calls.Load() != 1 {
		t.Fatalf("manual refresh did not intersect providers: %d %d", first.calls.Load(), second.calls.Load())
	}
	if _, err = c.RefreshBatch(ctx, catalog.RefreshScope{LibraryID: &lib1.ID, IncludeKnown: true, IgnoreWait: true}, 0); err != nil {
		t.Fatal(err)
	}
	if first.calls.Load() != 3 || second.calls.Load() != 1 {
		t.Fatalf("batch refresh did not intersect providers: %d %d", first.calls.Load(), second.calls.Load())
	}
	if _, err = l.UpdateLibrary(ctx, lib2.ID, "Two", []byte(`{"providers":[]}`)); err != nil {
		t.Fatal(err)
	}
	got, status, err := c.Refresh(ctx, ed.ID)
	if err != nil || status != catalog.StatusSkipped || got.SyncStatus != catalog.StatusSkipped || got.SyncedAt == nil {
		t.Fatalf("empty intersection must record a skipped refresh: %q %+v %v", status, got, err)
	}
	if first.calls.Load() != 3 || second.calls.Load() != 1 {
		t.Fatalf("empty provider list should block all lookups: %d %d", first.calls.Load(), second.calls.Load())
	}
}
