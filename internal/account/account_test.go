package account_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/larueli/biblisann/internal/account"
	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/catalog"
	"github.com/larueli/biblisann/internal/dbtest"
	"github.com/larueli/biblisann/internal/library"
	"github.com/larueli/biblisann/internal/lists"
	"github.com/larueli/biblisann/internal/loans"
	"github.com/larueli/biblisann/internal/reading"
	"github.com/larueli/biblisann/internal/storage"
)

type env struct {
	acc   *account.Service
	auth  *auth.Service
	lib   *library.Service
	cat   *catalog.Service
	lists *lists.Service
	loans *loans.Service
	rd    *reading.Service
}

func newEnv(t *testing.T) *env {
	pool := dbtest.Pool(t)
	covers, _ := storage.NewLocal(t.TempDir())
	a, _ := auth.NewService(pool, auth.RegistrationOpen)
	lib := library.NewService(pool)
	cat := catalog.NewService(pool, catalog.Config{Covers: covers, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	ls, ln, rd := lists.NewService(pool, lib), loans.NewService(pool, lib), reading.NewService(pool)
	return &env{acc: account.NewService(pool, lib, cat, ls, ln, rd), auth: a, lib: lib, cat: cat, lists: ls, loans: ln, rd: rd}
}

func (e *env) user(t *testing.T, email string, admin bool) (context.Context, uuid.UUID) {
	u, err := e.auth.CreateUser(context.Background(), email, "tulips in the garden", email, admin)
	if err != nil {
		t.Fatal(err)
	}
	return authz.With(context.Background(), &authz.Principal{UserID: u.ID, Email: email, IsAdmin: admin, Via: "session"}), u.ID
}

func png8() []byte {
	var b bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 8, 12))
	for x := range 8 {
		for y := range 12 {
			img.Set(x, y, color.RGBA{R: 200, A: 255})
		}
	}
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func TestLibraryRoundTrip(t *testing.T) {
	e := newEnv(t)
	a, aid := e.user(t, "a@example.org", true)
	b, _ := e.user(t, "b@example.org", false)

	lib, _ := e.lib.CreateLibrary(a, "Home")
	house, _ := e.lib.CreatePlace(a, lib.ID, library.PlaceInput{Name: "House"})
	shelf, _ := e.lib.CreatePlace(a, lib.ID, library.PlaceInput{Name: "Shelf", ParentID: &house.ID})
	ed1, _, _ := e.cat.Create(a, aid, catalog.EditionInput{ISBN: "9782070612758", Title: "Le Petit Prince", Authors: []string{"Saint-Exupéry"}}, "", nil)
	cover := e.cat.StoreCover(a, png8())
	if cover == nil {
		t.Fatal("cover")
	}
	if err := e.cat.AttachCover(a, ed1.ID, *cover); err != nil {
		t.Fatal(err)
	}
	ed2, _, _ := e.cat.Create(a, aid, catalog.EditionInput{Title: "Zine", Authors: []string{"Me"}, Series: "Zines", SeriesPos: ptr(2)}, "", nil)
	i1, _ := e.lib.AddInstance(a, lib.ID, library.InstanceInput{EditionID: ed1.ID, PlaceID: &shelf.ID, Notes: "signed, <b>first</b> print"}, "")
	_, _ = e.lib.AddInstance(a, lib.ID, library.InstanceInput{EditionID: ed2.ID}, "")
	ll, _ := e.lists.Create(a, "To lend", &lib.ID)
	_, _ = e.lists.AddItem(a, ll.ID, lists.ItemInput{Title: "Wanted"})
	past := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	loan, err := e.loans.Lend(a, loans.LendInput{InstanceID: i1.ID, Counterparty: "Marie", LentOn: &past})
	if err != nil {
		t.Fatal(err)
	}
	ret := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	_, _ = e.loans.Return(a, loan.ID, &ret)
	_, _ = e.loans.Borrow(a, loans.BorrowInput{LibraryID: lib.ID, Title: "Library book", Counterparty: "City library"})

	var buf bytes.Buffer
	if err := e.acc.ExportLibrary(a, lib.ID, &buf); err != nil {
		t.Fatal(err)
	}
	// someone without access cannot export it
	if err := e.acc.ExportLibrary(b, lib.ID, io.Discard); !errors.Is(err, authz.ErrNotFound) {
		t.Errorf("foreign export: %v", err)
	}

	rep, err := e.acc.ImportLibrary(b, bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Libraries != 1 || rep.Instances != 2 || rep.Places != 2 || rep.Lists != 1 || rep.ListItems != 1 || rep.Loans != 2 {
		t.Fatalf("report %+v", rep)
	}
	page, _ := e.lib.ListInstances(b, rep.LibraryIDs[0], "", 10)
	if len(page.Items) != 2 {
		t.Fatalf("instances: %d", len(page.Items))
	}
	var withCover, withPlace, withNotes bool
	for _, it := range page.Items {
		withCover = withCover || it.CoverID != nil
		withPlace = withPlace || it.PlaceID != nil
		withNotes = withNotes || strings.Contains(it.Notes, "signed")
	}
	if !withCover || !withPlace || !withNotes {
		t.Errorf("cover=%v place=%v notes=%v", withCover, withPlace, withNotes)
	}
	open, _ := e.loans.List(b, true)
	if len(open) != 2 {
		t.Errorf("loans: %d", len(open))
	}
	var returned bool
	for _, l := range open {
		returned = returned || (l.ReturnedOn != nil && l.LentOn.Equal(past))
	}
	if !returned {
		t.Error("returned loan lost its dates")
	}
	// importing twice creates a second library but shares the catalog editions
	rep2, _ := e.acc.ImportLibrary(b, bytes.NewReader(buf.Bytes()))
	if rep2.Editions != 0 || rep2.Libraries != 1 {
		t.Errorf("second import: %+v", rep2)
	}

	// wrong or hostile archives
	for name, data := range map[string]io.Reader{"garbage": strings.NewReader("not a tar"), "empty": bytes.NewReader(nil)} {
		if _, err := e.acc.ImportLibrary(b, data); !errors.Is(err, account.ErrArchive) {
			t.Errorf("%s: %v", name, err)
		}
	}
	var acct bytes.Buffer
	_ = e.acc.ExportAccount(a, &acct)
	if _, err := e.acc.ImportLibrary(b, bytes.NewReader(acct.Bytes())); !errors.Is(err, account.ErrArchive) {
		t.Errorf("account archive as library: %v", err)
	}
}

func ptr(f float64) *float64 { return &f }

func TestAccountRoundTrip(t *testing.T) {
	e := newEnv(t)
	a, aid := e.user(t, "a@example.org", true)
	b, _ := e.user(t, "b@example.org", false)
	lib, _ := e.lib.CreateLibrary(a, "Home")
	ed, _, _ := e.cat.Create(a, aid, catalog.EditionInput{ISBN: "9780306406157", Title: "Dune", Authors: []string{"Frank Herbert"}}, "", nil)
	_, _ = e.lib.AddInstance(a, lib.ID, library.InstanceInput{EditionID: ed.ID}, "")
	_, _ = e.rd.SetStatus(a, ed.WorkID, "read", nil, nil)
	g := 9
	_, _ = e.rd.SetGrade(a, ed.WorkID, &g, "Superb")
	_, _ = e.loans.CreateContact(a, "Marie", "m@example.org", "")
	wl, _ := e.lists.Wishlist(a)
	_, _ = e.lists.AddItem(a, wl.ID, lists.ItemInput{Title: "Hobbit", Authors: []string{"Tolkien"}})
	pl, _ := e.lists.Create(a, "Summer", nil)
	_, _ = e.lists.AddItem(a, pl.ID, lists.ItemInput{Title: "Beach book"})

	var buf bytes.Buffer
	if err := e.acc.ExportAccount(a, &buf); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"tulips in the garden", "argon2id"} {
		if strings.Contains(buf.String(), secret) {
			t.Errorf("export leaks %q", secret)
		}
	}
	rep, err := e.acc.ImportAccount(b, bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Libraries != 1 || rep.Instances != 1 || rep.Contacts != 1 || rep.Reading != 1 || rep.Grades != 1 || rep.Lists != 1 || rep.ListItems != 2 {
		t.Fatalf("report %+v", rep)
	}
	st, _ := e.rd.Stats(b)
	if st.ByStatus["read"] != 1 || st.AverageGrade != 9 {
		t.Errorf("stats %+v", st)
	}
	items, _ := e.lists.Items(b, mustWishlist(t, e, b))
	if len(items) != 1 || items[0].Title != "Hobbit" {
		t.Errorf("wishlist: %+v", items)
	}
}

func mustWishlist(t *testing.T, e *env, ctx context.Context) uuid.UUID {
	w, err := e.lists.Wishlist(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return w.ID
}

func TestDeleteAccount(t *testing.T) {
	e := newEnv(t)
	admin, adminID := e.user(t, "admin@example.org", true)
	_, _ = e.user(t, "second-admin@example.org", true)
	a, aid := e.user(t, "a@example.org", false)
	b, bid := e.user(t, "b@example.org", false)
	c, _ := e.user(t, "c@example.org", false)

	solo, _ := e.lib.CreateLibrary(a, "Solo")
	shared, _ := e.lib.CreateLibrary(a, "Shared")
	if err := e.lib.ShareWithUser(a, shared.ID, "b@example.org", library.Grant{Role: "edit"}); err != nil {
		t.Fatal(err)
	}
	ed, _, _ := e.cat.Create(a, aid, catalog.EditionInput{Title: "Zine"}, "", nil)
	_, _ = e.lib.AddInstance(a, solo.ID, library.InstanceInput{EditionID: ed.ID}, "")

	// strangers cannot delete someone else
	if err := e.acc.Delete(c, aid, false); !errors.Is(err, authz.ErrForbidden) {
		t.Errorf("stranger: %v", err)
	}
	// the sole admin of a shared library must decide first
	var sole *account.ErrSoleAdmin
	if err := e.acc.Delete(a, aid, false); !errors.As(err, &sole) || len(sole.Libraries) != 1 || sole.Libraries[0] != "Shared" {
		t.Fatalf("sole admin: %v", err)
	}
	if _, err := e.lib.GetLibrary(a, solo.ID); err != nil {
		t.Errorf("a refused deletion must change nothing: %v", err)
	}
	// with promotion b inherits the shared library; the solo library disappears
	if err := e.acc.Delete(a, aid, true); err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.GetLibrary(b, shared.ID); err != nil {
		t.Errorf("b lost the shared library: %v", err)
	}
	if _, err := e.lib.UpdateLibrary(b, shared.ID, "Renamed", []byte(`{}`)); err != nil {
		t.Errorf("b should now administer it: %v", err)
	}
	if _, err := e.auth.Login(context.Background(), "a@example.org", "tulips in the garden", ""); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("deleted account can still sign in: %v", err)
	}
	// an administrator can delete others, but never the last administrator
	if err := e.acc.Delete(admin, bid, false); err != nil {
		t.Errorf("admin deletes b (its library is now sole-admin-b with no other member): %v", err)
	}
	if err := e.acc.Delete(admin, adminID, false); err != nil {
		t.Errorf("one of two admins may leave: %v", err)
	}
}

func TestLastInstanceAdminCannotBeDeleted(t *testing.T) {
	e := newEnv(t)
	admin, id := e.user(t, "admin@example.org", true)
	if err := e.acc.Delete(admin, id, true); !errors.Is(err, account.ErrLastInstanceAdmin) {
		t.Errorf("last admin: %v", err)
	}
}
