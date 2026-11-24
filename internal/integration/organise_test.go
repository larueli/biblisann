package integration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/catalog"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/dbtest"
	"github.com/larueli/biblisann/internal/library"
	"github.com/larueli/biblisann/internal/lists"
	"github.com/larueli/biblisann/internal/loans"
	"github.com/larueli/biblisann/internal/reading"
	"github.com/larueli/biblisann/internal/tokens"
)

type env struct {
	pool    *pgxpool.Pool
	auth    *auth.Service
	lib     *library.Service
	cat     *catalog.Service
	lists   *lists.Service
	loans   *loans.Service
	reading *reading.Service
	tokens  *tokens.Service
}

func newEnv(t *testing.T) *env {
	pool := dbtest.Pool(t)
	a, _ := auth.NewService(pool, auth.RegistrationOpen)
	lib := library.NewService(pool)
	return &env{pool: pool, auth: a, lib: lib, cat: catalog.NewService(pool, catalog.Config{}),
		lists: lists.NewService(pool, lib), loans: loans.NewService(pool, lib), reading: reading.NewService(pool), tokens: tokens.NewService(pool)}
}

func (e *env) user(t *testing.T, email string) (context.Context, uuid.UUID) {
	u, err := e.auth.Register(context.Background(), email, "tulips in the garden", email)
	if err != nil {
		t.Fatal(err)
	}
	return authz.With(context.Background(), &authz.Principal{UserID: u.ID, Email: email, Via: "session"}), u.ID
}

func (e *env) book(t *testing.T, uid uuid.UUID, isbn, title, series string, pos *float64) dbsqlc.Edition {
	ed, _, err := e.cat.Create(context.Background(), uid, catalog.EditionInput{ISBN: isbn, Title: title, Authors: []string{"Au"}, Series: series, SeriesPos: pos}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return ed
}

func fp(f float64) *float64 { return &f }

func TestReadingGradesStats(t *testing.T) {
	e := newEnv(t)
	ctx, uid := e.user(t, "a@example.org")
	other, _ := e.user(t, "b@example.org")
	ed := e.book(t, uid, "", "Dune", "", nil)

	st, err := e.reading.SetStatus(ctx, ed.WorkID, "reading", nil, nil)
	if err != nil || st.StartedOn == nil {
		t.Fatalf("reading: %+v %v", st, err)
	}
	fin := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	start := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if _, err := e.reading.SetStatus(ctx, ed.WorkID, "read", &start, &fin); err != nil {
		t.Fatal(err)
	}
	if _, err := e.reading.SetStatus(ctx, ed.WorkID, "read", &fin, &start); !errors.Is(err, reading.ErrInvalid) {
		t.Errorf("finish before start: %v", err)
	}
	if _, err := e.reading.SetStatus(ctx, ed.WorkID, "nope", nil, nil); !errors.Is(err, reading.ErrInvalid) {
		t.Errorf("bad status: %v", err)
	}
	if _, err := e.reading.SetStatus(ctx, uuid.New(), "read", nil, nil); !errors.Is(err, authz.ErrNotFound) {
		t.Errorf("unknown work: %v", err)
	}
	g := 9
	if _, err := e.reading.SetGrade(ctx, ed.WorkID, &g, "Superb"); err != nil {
		t.Fatal(err)
	}
	bad := 11
	if _, err := e.reading.SetGrade(ctx, ed.WorkID, &bad, ""); !errors.Is(err, reading.ErrInvalid) {
		t.Errorf("grade 11: %v", err)
	}
	ws, _ := e.reading.Get(ctx, ed.WorkID)
	if ws.Reading.Status != "read" || ws.Grade == nil || *ws.Grade.Grade != 9 {
		t.Errorf("work state: %+v", ws)
	}
	// per-user isolation
	if ws, _ := e.reading.Get(other, ed.WorkID); ws.Reading != nil || ws.Grade != nil {
		t.Errorf("another user sees my state: %+v", ws)
	}
	stats, _ := e.reading.Stats(ctx)
	if stats.ByStatus["read"] != 1 || stats.ReadPerYear[2026] != 1 || stats.AverageGrade != 9 || stats.Graded != 1 {
		t.Errorf("stats: %+v", stats)
	}
}

func TestListsWishlistAndSeries(t *testing.T) {
	e := newEnv(t)
	ctx, uid := e.user(t, "a@example.org")
	bob, _ := e.user(t, "b@example.org")
	lib, _ := e.lib.CreateLibrary(ctx, "Home")
	for _, n := range []float64{1, 2, 5} {
		ed := e.book(t, uid, "", "Tintin "+string(rune('0'+int(n))), "Tintin", fp(n))
		if _, err := e.lib.AddInstance(ctx, lib.ID, library.InstanceInput{EditionID: ed.ID}, ""); err != nil {
			t.Fatal(err)
		}
	}
	wl, err := e.lists.Wishlist(ctx)
	if err != nil || wl.Kind != "wishlist" {
		t.Fatal(wl, err)
	}
	if wl2, _ := e.lists.Wishlist(ctx); wl2.ID != wl.ID {
		t.Error("wishlist must be unique")
	}
	series, err := e.lists.Series(ctx, lib.ID)
	if err != nil || len(series) != 1 || len(series[0].Missing) != 2 || series[0].Missing[0] != 3 || series[0].Missing[1] != 4 {
		t.Fatalf("series: %+v %v", series, err)
	}
	n, err := e.lists.AddMissingToList(ctx, lib.ID, "Tintin", wl.ID)
	if err != nil || n != 2 {
		t.Fatalf("missing to list: %d %v", n, err)
	}
	if n, _ := e.lists.AddMissingToList(ctx, lib.ID, "Tintin", wl.ID); n != 0 {
		t.Errorf("second run must add nothing, added %d", n)
	}
	items, _ := e.lists.Items(ctx, wl.ID)
	if len(items) != 2 || items[0].InLibrary {
		t.Errorf("items: %+v", items)
	}
	// An item for a book I own shows in_library.
	ed := e.book(t, uid, "9782070612758", "Owned", "", nil)
	_, _ = e.lib.AddInstance(ctx, lib.ID, library.InstanceInput{EditionID: ed.ID}, "")
	if _, err := e.lists.AddItem(ctx, wl.ID, lists.ItemInput{EditionID: &ed.ID}); err != nil {
		t.Fatal(err)
	}
	items, _ = e.lists.Items(ctx, wl.ID)
	if !items[len(items)-1].InLibrary {
		t.Error("owned item should be flagged")
	}
	// Bob cannot see or modify Alice's wishlist; deleting the wishlist is refused.
	if _, err := e.lists.Items(bob, wl.ID); !errors.Is(err, authz.ErrNotFound) {
		t.Errorf("foreign list: %v", err)
	}
	if err := e.lists.Delete(ctx, wl.ID); err == nil {
		t.Error("wishlist deletion must be refused")
	}
	// Library lists follow library permissions.
	ll, err := e.lists.Create(ctx, "To buy together", &lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.lists.Items(bob, ll.ID); !errors.Is(err, authz.ErrNotFound) {
		t.Errorf("non-member sees library list: %v", err)
	}
}

func TestLoans(t *testing.T) {
	e := newEnv(t)
	ctx, uid := e.user(t, "a@example.org")
	lib, _ := e.lib.CreateLibrary(ctx, "Home")
	ed := e.book(t, uid, "", "Dune", "", nil)
	inst, _ := e.lib.AddInstance(ctx, lib.ID, library.InstanceInput{EditionID: ed.ID}, "")
	c, err := e.loans.CreateContact(ctx, "Marie", "marie@example.org", "")
	if err != nil {
		t.Fatal(err)
	}
	due := time.Now().Add(14 * 24 * time.Hour).UTC().Truncate(24 * time.Hour)
	l, err := e.loans.Lend(ctx, loans.LendInput{InstanceID: inst.ID, ContactID: &c.ID, DueOn: &due})
	if err != nil || l.Counterparty != "Marie" || l.Title != "Dune" {
		t.Fatalf("lend: %+v %v", l, err)
	}
	if _, err := e.loans.Lend(ctx, loans.LendInput{InstanceID: inst.ID, Counterparty: "Paul"}); !errors.Is(err, loans.ErrAlreadyLent) {
		t.Errorf("double lending: %v", err)
	}
	if _, err := e.loans.Borrow(ctx, loans.BorrowInput{LibraryID: lib.ID, Title: "Library book", Counterparty: "City library", DueOn: &due}); err != nil {
		t.Fatal(err)
	}
	open, _ := e.loans.List(ctx, false)
	if len(open) != 2 {
		t.Errorf("open loans: %d", len(open))
	}
	if _, err := e.loans.Return(ctx, l.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.loans.Return(ctx, l.ID, nil); !errors.Is(err, loans.ErrAlreadyReturn) {
		t.Errorf("double return: %v", err)
	}
	if _, err := e.loans.Lend(ctx, loans.LendInput{InstanceID: inst.ID, Counterparty: "Paul"}); err != nil {
		t.Errorf("lend again after return: %v", err)
	}
	// Someone else's contact or loan is invisible.
	bob, _ := e.user(t, "b@example.org")
	if _, err := e.loans.Return(bob, l.ID, nil); !errors.Is(err, authz.ErrNotFound) {
		t.Errorf("foreign loan: %v", err)
	}
	if _, err := e.loans.Lend(bob, loans.LendInput{InstanceID: inst.ID, Counterparty: "x"}); !errors.Is(err, authz.ErrNotFound) {
		t.Errorf("lending a copy of a library I cannot see: %v", err)
	}
}

func TestTokens(t *testing.T) {
	e := newEnv(t)
	ctx, _ := e.user(t, "a@example.org")
	lib, _ := e.lib.CreateLibrary(ctx, "Home")
	c, err := e.tokens.Create(ctx, tokens.KindPAT, "script", []string{"read"}, []uuid.UUID{lib.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.tokens.Resolve(context.Background(), c.Token)
	if err != nil || p == nil || p.Via != "token" || len(p.Scopes) != 1 || !p.AllowsLibrary(lib.ID) {
		t.Fatalf("resolve: %+v %v", p, err)
	}
	if p, _ := e.tokens.Resolve(context.Background(), c.Token+"x"); p != nil {
		t.Error("wrong token accepted")
	}
	// Tokens cannot mint tokens, and feed tokens do not authenticate the API.
	tokCtx := authz.With(context.Background(), p)
	if _, err := e.tokens.Create(tokCtx, tokens.KindPAT, "child", []string{"read"}, nil, nil); !errors.Is(err, authz.ErrForbidden) {
		t.Errorf("token minting token: %v", err)
	}
	f, _ := e.tokens.Create(ctx, tokens.KindFeed, "calendar", nil, nil, nil)
	if p, _ := e.tokens.Resolve(context.Background(), f.Token); p != nil {
		t.Error("feed token must not authenticate the API")
	}
	if _, err := e.tokens.FeedUser(context.Background(), f.Token); err != nil {
		t.Error(err)
	}
	if _, err := e.tokens.FeedUser(context.Background(), c.Token); err == nil {
		t.Error("PAT must not open the feed")
	}
	past := time.Now().Add(-time.Hour)
	if _, err := e.tokens.Create(ctx, tokens.KindPAT, "old", []string{"read"}, nil, &past); !errors.Is(err, tokens.ErrInvalid) {
		t.Errorf("expired at creation: %v", err)
	}
	if err := e.tokens.Revoke(ctx, c.Meta.ID); err != nil {
		t.Fatal(err)
	}
	if p, _ := e.tokens.Resolve(context.Background(), c.Token); p != nil {
		t.Error("revoked token still works")
	}
}

func TestSharedLoansAreManagedByEveryEditor(t *testing.T) {
	e := newEnv(t)
	ctx, uid := e.user(t, "a@example.org")
	editor, editorID := e.user(t, "b@example.org")
	reader, _ := e.user(t, "c@example.org")
	lib, _ := e.lib.CreateLibrary(ctx, "Home")
	for email, role := range map[string]string{"b@example.org": "edit", "c@example.org": "read"} {
		if err := e.lib.ShareWithUser(ctx, lib.ID, email, library.Grant{Role: role}); err != nil {
			t.Fatal(err)
		}
	}
	ed := e.book(t, uid, "", "Dune", "", nil)
	inst, _ := e.lib.AddInstance(ctx, lib.ID, library.InstanceInput{EditionID: ed.ID}, "")
	l, err := e.loans.Lend(ctx, loans.LendInput{InstanceID: inst.ID, Counterparty: "Marie"})
	if err != nil {
		t.Fatal(err)
	}

	if rows, _ := e.loans.List(editor, false); len(rows) != 1 {
		t.Errorf("editor sees %d loans, want 1", len(rows))
	}
	if rows, _ := e.loans.List(reader, false); len(rows) != 0 {
		t.Errorf("reader sees %d loans, want 0", len(rows))
	}
	if _, err := e.loans.Return(reader, l.ID, nil); !errors.Is(err, authz.ErrNotFound) {
		t.Errorf("reader returns a loan: %v", err)
	}
	if _, err := e.loans.Return(editor, l.ID, nil); err != nil {
		t.Errorf("editor returns a loan of a shared library: %v", err)
	}

	// A member who lost access cannot touch the loans they recorded.
	l2, err := e.loans.Lend(editor, loans.LendInput{InstanceID: inst.ID, Counterparty: "Paul"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.lib.UnshareUser(ctx, lib.ID, editorID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.loans.Return(editor, l2.ID, nil); !errors.Is(err, authz.ErrNotFound) {
		t.Errorf("former member returns a loan: %v", err)
	}
	if rows, _ := e.loans.List(editor, false); len(rows) != 0 {
		t.Errorf("former member still lists %d loans", len(rows))
	}
	if _, err := e.loans.Return(ctx, l2.ID, nil); err != nil {
		t.Errorf("administrator returns it: %v", err)
	}
}
