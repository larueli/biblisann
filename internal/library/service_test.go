package library_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/catalog"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/dbtest"
	"github.com/larueli/biblisann/internal/library"
)

type env struct {
	pool *pgxpool.Pool
	lib  *library.Service
	cat  *catalog.Service
	auth *auth.Service
}

func newEnv(t *testing.T) *env {
	pool := dbtest.Pool(t)
	a, _ := auth.NewService(pool, auth.RegistrationOpen)
	return &env{pool: pool, lib: library.NewService(pool), cat: catalog.NewService(pool, catalog.Config{}), auth: a}
}

func (e *env) user(t *testing.T, email string) (context.Context, uuid.UUID) {
	u, err := e.auth.Register(context.Background(), email, "tulips in the garden", email)
	if err != nil {
		t.Fatal(err)
	}
	return authz.With(context.Background(), &authz.Principal{UserID: u.ID, Email: email, Via: "session"}), u.ID
}

func (e *env) edition(t *testing.T, uid uuid.UUID, isbn, title string, authors ...string) dbsqlc.Edition {
	ed, _, err := e.cat.Create(context.Background(), uid, catalog.EditionInput{ISBN: isbn, Title: title, Authors: authors}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return ed
}

func TestMembershipAndDenyByDefault(t *testing.T) {
	e := newEnv(t)
	alice, aliceID := e.user(t, "alice@example.org")
	bob, _ := e.user(t, "bob@example.org")

	lib, err := e.lib.CreateLibrary(alice, "Home")
	if err != nil {
		t.Fatal(err)
	}
	ed := e.edition(t, aliceID, "9782070612758", "Le Petit Prince", "Saint-Exupéry")
	inst, err := e.lib.AddInstance(alice, lib.ID, library.InstanceInput{EditionID: ed.ID}, "")
	if err != nil {
		t.Fatal(err)
	}

	// Bob is not a member: he must not even learn the library exists.
	for name, err := range map[string]error{
		"get":  func() error { _, err := e.lib.GetLibrary(bob, lib.ID); return err }(),
		"list": func() error { _, err := e.lib.ListInstances(bob, lib.ID, "", 10); return err }(),
		"add": func() error {
			_, err := e.lib.AddInstance(bob, lib.ID, library.InstanceInput{EditionID: ed.ID}, "")
			return err
		}(),
		"search": func() error { _, err := e.lib.SearchInstances(bob, lib.ID, "prince", 10); return err }(),
		"sync":   func() error { _, err := e.lib.SyncIndex(bob, lib.ID, 0, 10); return err }(),
		"inst":   func() error { _, err := e.lib.GetInstance(bob, inst.ID); return err }(),
		"del":    e.lib.DeleteInstance(bob, inst.ID),
	} {
		if !errors.Is(err, authz.ErrNotFound) {
			t.Errorf("%s as non-member: %v, want not found", name, err)
		}
	}
	if _, err := e.lib.ListLibraries(context.Background()); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Errorf("anonymous: %v", err)
	}

	// Read-only member can read but not write.
	q := dbsqlc.New(e.pool)
	bobP, _ := authz.From(bob)
	_ = q.AddLibraryMember(context.Background(), dbsqlc.AddLibraryMemberParams{LibraryID: lib.ID, UserID: bobP.UserID, Role: "read"})
	if _, err := e.lib.ListInstances(bob, lib.ID, "", 10); err != nil {
		t.Errorf("reader list: %v", err)
	}
	if _, err := e.lib.AddInstance(bob, lib.ID, library.InstanceInput{EditionID: ed.ID}, ""); !errors.Is(err, authz.ErrForbidden) {
		t.Errorf("reader add: %v, want forbidden", err)
	}
	if _, err := e.lib.UpdateLibrary(bob, lib.ID, "x", []byte(`{}`)); !errors.Is(err, authz.ErrForbidden) {
		t.Errorf("reader update library: %v", err)
	}
}

func TestTokenScopes(t *testing.T) {
	e := newEnv(t)
	owner, uid := e.user(t, "o@example.org")
	lib, _ := e.lib.CreateLibrary(owner, "L")
	ed := e.edition(t, uid, "", "Zine")
	roCtx := authz.With(context.Background(), &authz.Principal{UserID: uid, Scopes: []string{"read"}, Via: "token"})
	if _, err := e.lib.ListInstances(roCtx, lib.ID, "", 10); err != nil {
		t.Errorf("read scope list: %v", err)
	}
	if _, err := e.lib.AddInstance(roCtx, lib.ID, library.InstanceInput{EditionID: ed.ID}, ""); !errors.Is(err, authz.ErrForbidden) {
		t.Errorf("read-only token must not write: %v", err)
	}
}

func TestIdempotentAdd(t *testing.T) {
	e := newEnv(t)
	ctx, uid := e.user(t, "a@example.org")
	lib, _ := e.lib.CreateLibrary(ctx, "L")
	ed := e.edition(t, uid, "", "Zine")
	other := e.edition(t, uid, "", "Other")

	a, err := e.lib.AddInstance(ctx, lib.ID, library.InstanceInput{EditionID: ed.ID}, "key-12345678")
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.lib.AddInstance(ctx, lib.ID, library.InstanceInput{EditionID: ed.ID}, "key-12345678")
	if err != nil || a.ID != b.ID {
		t.Fatalf("replay must return the original: %v %v %v", a.ID, b.ID, err)
	}
	if _, err := e.lib.AddInstance(ctx, lib.ID, library.InstanceInput{EditionID: other.ID}, "key-12345678"); !errors.Is(err, library.ErrIdempotencyConflict) {
		t.Errorf("different body, same key: %v", err)
	}
	page, _ := e.lib.ListInstances(ctx, lib.ID, "", 10)
	if len(page.Items) != 1 {
		t.Errorf("replay created %d copies", len(page.Items))
	}
	if _, err := e.lib.AddInstance(ctx, lib.ID, library.InstanceInput{EditionID: ed.ID}, "short"); !errors.Is(err, library.ErrInvalid) {
		t.Errorf("short key: %v", err)
	}
}

func TestOwnedAtWorkLevelAndSearch(t *testing.T) {
	e := newEnv(t)
	ctx, uid := e.user(t, "a@example.org")
	lib, _ := e.lib.CreateLibrary(ctx, "Home")
	paper := e.edition(t, uid, "9782070612758", "Le Petit Prince", "Saint-Exupéry")
	if _, err := e.lib.AddInstance(ctx, lib.ID, library.InstanceInput{EditionID: paper.ID}, ""); err != nil {
		t.Fatal(err)
	}
	// Another edition of the same work (same title and authors), not owned.
	pocket := e.edition(t, uid, "9782075089722", "Le Petit Prince", "Saint-Exupéry")
	if pocket.WorkID != paper.WorkID {
		t.Fatal("editions should share the work")
	}

	m, err := e.lib.Owned(ctx, "978-2-07-508972-2")
	if err != nil || len(m) != 1 || m[0].SameEdition || m[0].LibraryName != "Home" {
		t.Errorf("other edition owned: %+v %v", m, err)
	}
	m, _ = e.lib.Owned(ctx, "9782070612758")
	if len(m) != 1 || !m[0].SameEdition {
		t.Errorf("same edition: %+v", m)
	}
	if m, _ := e.lib.Owned(ctx, "9780306406157"); len(m) != 0 {
		t.Errorf("unknown book: %+v", m)
	}
	// Another user does not see Alice's copies.
	bob, _ := e.user(t, "b@example.org")
	if m, _ := e.lib.Owned(bob, "9782070612758"); len(m) != 0 {
		t.Errorf("ownership leaked across users: %+v", m)
	}

	for _, q := range []string{"petit prince", "PETIT", "saint exupery", "prnce", "9782070612758"} {
		rows, err := e.lib.SearchInstances(ctx, lib.ID, q, 10)
		if err != nil || len(rows) != 1 {
			t.Errorf("search %q: %d rows, %v", q, len(rows), err)
		}
	}
	if rows, _ := e.lib.SearchInstances(ctx, lib.ID, "zzzzqq", 10); len(rows) != 0 {
		t.Error("nonsense query matched")
	}
}

func TestPaginationAndSyncTombstones(t *testing.T) {
	e := newEnv(t)
	ctx, uid := e.user(t, "a@example.org")
	lib, _ := e.lib.CreateLibrary(ctx, "L")
	var ids []uuid.UUID
	for i := range 5 {
		ed := e.edition(t, uid, "", "Book "+string(rune('A'+i)))
		inst, _ := e.lib.AddInstance(ctx, lib.ID, library.InstanceInput{EditionID: ed.ID}, "")
		ids = append(ids, inst.ID)
	}
	p1, _ := e.lib.ListInstances(ctx, lib.ID, "", 2)
	if len(p1.Items) != 2 || p1.NextCursor == "" {
		t.Fatalf("page 1: %d %q", len(p1.Items), p1.NextCursor)
	}
	p2, _ := e.lib.ListInstances(ctx, lib.ID, p1.NextCursor, 2)
	p3, _ := e.lib.ListInstances(ctx, lib.ID, p2.NextCursor, 2)
	if len(p2.Items) != 2 || len(p3.Items) != 1 || p3.NextCursor != "" {
		t.Errorf("pagination: %d %d %d", len(p1.Items), len(p2.Items), len(p3.Items))
	}
	if _, err := e.lib.ListInstances(ctx, lib.ID, "garbage!!", 2); !errors.Is(err, library.ErrInvalid) {
		t.Errorf("bad cursor: %v", err)
	}

	full, err := e.lib.SyncIndex(ctx, lib.ID, 0, 100)
	if err != nil || len(full.Entries) != 5 || full.Cursor == 0 {
		t.Fatalf("full sync: %+v %v", full, err)
	}
	if err := e.lib.DeleteInstance(ctx, ids[0]); err != nil {
		t.Fatal(err)
	}
	delta, _ := e.lib.SyncIndex(ctx, lib.ID, full.Cursor, 100)
	if len(delta.Entries) != 1 || !delta.Entries[0].Deleted || delta.Entries[0].ID != ids[0] {
		t.Errorf("tombstone missing: %+v", delta)
	}
	if err := e.lib.RestoreInstance(ctx, ids[0]); err != nil {
		t.Fatal(err)
	}
	delta2, _ := e.lib.SyncIndex(ctx, lib.ID, delta.Cursor, 100)
	if len(delta2.Entries) != 1 || delta2.Entries[0].Deleted || delta2.Entries[0].Title == "" {
		t.Errorf("restore not synced: %+v", delta2)
	}
	empty, _ := e.lib.SyncIndex(ctx, lib.ID, delta2.Cursor, 100)
	if len(empty.Entries) != 0 || empty.Cursor != delta2.Cursor {
		t.Errorf("nothing new: %+v", empty)
	}
}

func TestPlacesTreeRules(t *testing.T) {
	e := newEnv(t)
	ctx, uid := e.user(t, "a@example.org")
	lib, _ := e.lib.CreateLibrary(ctx, "L")
	other, _ := e.lib.CreateLibrary(ctx, "Other")
	house, err := e.lib.CreatePlace(ctx, lib.ID, library.PlaceInput{Name: "House"})
	if err != nil {
		t.Fatal(err)
	}
	room, err := e.lib.CreatePlace(ctx, lib.ID, library.PlaceInput{Name: "Office", ParentID: &house.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.UpdatePlace(ctx, house.ID, library.PlaceInput{Name: "House", ParentID: &room.ID}); !errors.Is(err, library.ErrInvalid) {
		t.Errorf("cycle accepted: %v", err)
	}
	if _, err := e.lib.CreatePlace(ctx, other.ID, library.PlaceInput{Name: "x", ParentID: &house.ID}); !errors.Is(err, library.ErrInvalid) {
		t.Errorf("cross-library parent accepted: %v", err)
	}
	ed := e.edition(t, uid, "", "Zine")
	inst, err := e.lib.AddInstance(ctx, lib.ID, library.InstanceInput{EditionID: ed.ID, PlaceID: &room.ID}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.AddInstance(ctx, other.ID, library.InstanceInput{EditionID: ed.ID, PlaceID: &room.ID}, ""); !errors.Is(err, library.ErrInvalid) {
		t.Errorf("place of another library accepted: %v", err)
	}
	if err := e.lib.DeletePlace(ctx, room.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := e.lib.GetInstance(ctx, inst.ID)
	if got.PlaceID != nil {
		t.Error("deleting a place must unset the copies' place")
	}
}
