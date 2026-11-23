package hcsync_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/catalog"
	"github.com/larueli/biblisann/internal/dbtest"
	"github.com/larueli/biblisann/internal/hardcover"
	"github.com/larueli/biblisann/internal/hcsync"
	"github.com/larueli/biblisann/internal/reading"
)

// fakeHC is an in-memory Hardcover account.
type fakeHC struct {
	books   []hardcover.RemoteBook
	bookIDs map[string]int64 // isbn -> hardcover book id
	calls   int
	upserts []hardcover.RemoteBook
	meErr   error
	err     error
	nextID  int64
}

func (f *fakeHC) Me(context.Context) (int64, error) { return 7, f.meErr }
func (f *fakeHC) UserBooks(_ context.Context, offset, limit int) ([]hardcover.RemoteBook, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	end := min(offset+limit, len(f.books))
	if offset >= len(f.books) {
		return nil, nil
	}
	return f.books[offset:end], nil
}
func (f *fakeHC) BookIDByISBN(_ context.Context, isbn string) (int64, error) {
	f.calls++
	return f.bookIDs[isbn], nil
}
func (f *fakeHC) Upsert(_ context.Context, ubID, bookID int64, status int, rating *float64) (int64, error) {
	f.calls++
	if ubID == 0 {
		f.nextID++
		ubID = 1000 + f.nextID
	}
	f.upserts = append(f.upserts, hardcover.RemoteBook{UserBookID: ubID, BookID: bookID, StatusID: status, Rating: rating})
	return ubID, nil
}

func TestTwoWaySync(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	key := bytes.Repeat([]byte{9}, 32)
	a, _ := auth.NewService(pool, auth.RegistrationOpen)
	u, _ := a.Register(ctx, "me@example.org", "tulips in the garden", "Me")
	uctx := authz.With(ctx, &authz.Principal{UserID: u.ID, Via: "session"})
	cat := catalog.NewService(pool, catalog.Config{})
	rd := reading.NewService(pool)

	dune, _, _ := cat.Create(ctx, u.ID, catalog.EditionInput{ISBN: "9780306406157", Title: "Dune", Authors: []string{"FH"}}, "", nil)
	hobbit, _, _ := cat.Create(ctx, u.ID, catalog.EditionInput{ISBN: "9782070612758", Title: "Hobbit", Authors: []string{"JT"}}, "", nil)

	remote := &fakeHC{bookIDs: map[string]int64{"9780306406157": 11, "9782070612758": 22}}
	svc := hcsync.New(pool, key, func(string) hcsync.Remote { return remote }, rd)
	svc.Sleep = func(context.Context, time.Duration) error { return nil }

	// linking verifies the token, validates the mode and stores the token sealed
	if err := svc.Link(ctx, u.ID, "tok", "nonsense"); !errors.Is(err, hcsync.ErrMode) {
		t.Errorf("bad mode: %v", err)
	}
	remote.meErr = hardcover.ErrUnauthorized
	if err := svc.Link(ctx, u.ID, "bad", "both"); !errors.Is(err, hardcover.ErrUnauthorized) {
		t.Errorf("rejected token: %v", err)
	}
	remote.meErr = nil
	if err := svc.Link(ctx, u.ID, "secret-hardcover-token", "both"); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	_ = pool.QueryRow(ctx, "SELECT token_enc FROM hardcover_links WHERE user_id = $1", u.ID).Scan(&raw)
	if bytes.Contains(raw, []byte("secret-hardcover-token")) || len(raw) == 0 {
		t.Error("the token must be sealed at rest")
	}
	if (hcsync.New(pool, nil, nil, rd)).Available() {
		t.Error("no key, no sync")
	}

	// PUSH: a local status and grade reach Hardcover; nothing is sent twice
	_, _ = rd.SetStatus(uctx, dune.WorkID, "read", nil, nil)
	g := 9
	_, _ = rd.SetGrade(uctx, dune.WorkID, &g, "great")
	res, err := svc.SyncUser(ctx, u.ID)
	if err != nil || res.Pushed != 1 || len(remote.upserts) != 1 {
		t.Fatalf("push: %+v %v %d", res, err, len(remote.upserts))
	}
	if up := remote.upserts[0]; up.BookID != 11 || up.StatusID != 3 || up.Rating == nil || *up.Rating != 4.5 {
		t.Errorf("pushed entry: %+v", up)
	}
	remote.books = []hardcover.RemoteBook{{UserBookID: remote.upserts[0].UserBookID, BookID: 11, StatusID: 3, Rating: ptr(4.5), UpdatedAt: time.Now(), ISBNs: []string{"9780306406157"}}}
	remote.upserts = nil
	if res, _ := svc.SyncUser(ctx, u.ID); res.Pushed != 0 || res.Pulled != 0 || len(remote.upserts) != 0 {
		t.Errorf("an unchanged state must not be re-sent: %+v", res)
	}
	// a local change is sent as an update of the same entry
	_, _ = rd.SetStatus(uctx, dune.WorkID, "abandoned", nil, nil)
	remote.books[0].UpdatedAt = time.Now().Add(-time.Hour) // the remote copy did not change
	if res, _ := svc.SyncUser(ctx, u.ID); res.Pushed != 1 || remote.upserts[0].StatusID != 5 || remote.upserts[0].UserBookID != remote.books[0].UserBookID {
		t.Errorf("update: %+v %+v", res, remote.upserts)
	}
	remote.books[0].StatusID = 5 // Hardcover now reflects it

	// PULL: a remote-only change comes down; unknown books are ignored
	remote.books = append(remote.books,
		hardcover.RemoteBook{UserBookID: 2001, BookID: 22, StatusID: 2, Rating: ptr(3), UpdatedAt: time.Now(), ISBNs: []string{"9782070612758"}},
		hardcover.RemoteBook{UserBookID: 2002, BookID: 33, StatusID: 3, UpdatedAt: time.Now(), ISBNs: []string{"9790000000001"}})
	remote.upserts = nil
	res, err = svc.SyncUser(ctx, u.ID)
	if err != nil || res.Pulled != 1 {
		t.Fatalf("pull: %+v %v", res, err)
	}
	st, err := rd.Get(uctx, hobbit.WorkID)
	if err != nil || st.Reading == nil || st.Reading.Status != "reading" || st.Grade == nil || *st.Grade.Grade != 6 {
		t.Errorf("pulled state: %+v %v", st, err)
	}
	if len(remote.upserts) != 0 {
		t.Errorf("what was just pulled must not be pushed back: %+v", remote.upserts)
	}

	// CONFLICT: both sides changed Hobbit; the more recent edit wins
	_, _ = rd.SetStatus(uctx, hobbit.WorkID, "read", nil, nil) // local edit, now
	remote.books[1].StatusID = 5                               // remote edit, older than the local one
	remote.books[1].UpdatedAt = time.Now().Add(-time.Hour)
	if res, _ := svc.SyncUser(ctx, u.ID); res.Pulled != 0 || res.Pushed != 1 || remote.upserts[len(remote.upserts)-1].StatusID != 3 {
		t.Errorf("local newer must win and be pushed: %+v %+v", res, remote.upserts)
	}
	remote.books[1].StatusID = 3 // in step again
	_, _ = rd.SetStatus(uctx, hobbit.WorkID, "to_read", nil, nil)
	remote.books[1].StatusID = 5
	remote.books[1].UpdatedAt = time.Now().Add(time.Hour) // remote edit is newer
	remote.upserts = nil
	if res, _ := svc.SyncUser(ctx, u.ID); res.Pulled != 1 || len(remote.upserts) != 0 {
		t.Errorf("remote newer must win: %+v %+v", res, remote.upserts)
	}
	if st, _ := rd.Get(uctx, hobbit.WorkID); st.Reading.Status != "abandoned" {
		t.Errorf("after the remote win: %s", st.Reading.Status)
	}

	// pull-only never writes to Hardcover; push-only never changes local data
	if err := svc.SetMode(ctx, u.ID, "pull"); err != nil {
		t.Fatal(err)
	}
	_, _ = rd.SetStatus(uctx, dune.WorkID, "reading", nil, nil)
	remote.upserts = nil
	_, _ = svc.SyncUser(ctx, u.ID)
	if len(remote.upserts) != 0 {
		t.Errorf("pull mode pushed: %+v", remote.upserts)
	}
	if err := svc.SetMode(ctx, u.ID, "push"); err != nil {
		t.Fatal(err)
	}
	remote.books[0].StatusID = 3
	remote.books[0].UpdatedAt = time.Now().Add(time.Hour)
	_, _ = svc.SyncUser(ctx, u.ID)
	if s, _ := rd.Get(uctx, dune.WorkID); s.Reading.Status != "reading" {
		t.Errorf("push mode changed local data: %s", s.Reading.Status)
	}

	// failures are reported on the link, never panics: rejected token, then rate limit
	remote.err = hardcover.ErrUnauthorized
	if err := svc.SetMode(ctx, u.ID, "both"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SyncUser(ctx, u.ID); !errors.Is(err, hardcover.ErrUnauthorized) {
		t.Errorf("rejected token must surface: %v", err)
	}
	if s, _ := svc.Get(ctx, u.ID); s.LastError == "" {
		t.Error("the link must show why it stopped")
	}
	remote.err = &hardcover.RateLimitedError{After: 90 * time.Second}
	_, _ = pool.Exec(ctx, "UPDATE hardcover_links SET next_sync_at = now()")
	if _, err := svc.SyncUser(ctx, u.ID); err != nil {
		t.Errorf("rate limiting is not an error: %v", err)
	}
	var secs float64
	_ = pool.QueryRow(ctx, "SELECT extract(epoch FROM next_sync_at - now()) FROM hardcover_links WHERE user_id = $1", u.ID).Scan(&secs)
	if secs < 60 || secs > 100 {
		t.Errorf("the next run must wait for Retry-After, got %.0fs", secs)
	}

	// unlinking forgets everything
	if err := svc.Unlink(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if s, _ := svc.Get(ctx, u.ID); s.Linked {
		t.Error("still linked")
	}
	var n int
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM hardcover_book_state").Scan(&n)
	if n != 0 {
		t.Errorf("sync state remains: %d", n)
	}
}

func TestBudgetResumes(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	a, _ := auth.NewService(pool, auth.RegistrationOpen)
	u, _ := a.Register(ctx, "me@example.org", "tulips in the garden", "Me")
	uctx := authz.With(ctx, &authz.Principal{UserID: u.ID, Via: "session"})
	cat := catalog.NewService(pool, catalog.Config{})
	rd := reading.NewService(pool)
	remote := &fakeHC{bookIDs: map[string]int64{}}
	isbns := []string{"9780306406157", "9782070612758", "9791032703687"}
	for i, isbn := range isbns {
		e, _, _ := cat.Create(ctx, u.ID, catalog.EditionInput{ISBN: isbn, Title: "B" + isbn, Authors: []string{"A"}}, "", nil)
		_, _ = rd.SetStatus(uctx, e.WorkID, "read", nil, nil)
		remote.bookIDs[isbn] = int64(i + 1)
	}
	svc := hcsync.New(pool, bytes.Repeat([]byte{1}, 32), func(string) hcsync.Remote { return remote }, rd)
	svc.Sleep = func(context.Context, time.Duration) error { return nil }
	svc.Budget = 4 // two books need a lookup and an upsert each
	if err := svc.Link(ctx, u.ID, "t", "push"); err != nil {
		t.Fatal(err)
	}
	res, err := svc.SyncUser(ctx, u.ID)
	if err != nil || !res.More || res.Pushed != 2 || remote.calls != 4 {
		t.Fatalf("first run: %+v %v calls=%d", res, err, remote.calls)
	}
	res, _ = svc.SyncUser(ctx, u.ID)
	if res.Pushed != 1 || res.More {
		t.Errorf("second run finishes the work: %+v", res)
	}
}

func ptr(f float64) *float64 { return &f }
