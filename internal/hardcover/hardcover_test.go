package hardcover

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/larueli/biblisann/internal/dbtest"
	"github.com/larueli/biblisann/internal/safehttp"
)

type fake struct {
	status int
	body   string
	req    *http.Request
}

func (f *fake) Do(r *http.Request) (*safehttp.Response, error) {
	f.req = r
	return &safehttp.Response{Status: f.status, Body: io.NopCloser(strings.NewReader(f.body))}, nil
}

func TestFetch(t *testing.T) {
	f := &fake{status: 200, body: `{"data":{"editions":[{"book":{"slug":"dune","rating":4.31,"ratings_count":1200}}]}}`}
	s := &Service{http: f, token: "tok", url: endpoint}
	r, v, link, err := s.Fetch(context.Background(), "9780441172719")
	if err != nil || r != 4.31 || v != 1200 || link != "https://hardcover.app/books/dune" {
		t.Fatalf("%v %v %v %v", r, v, link, err)
	}
	if f.req.Header.Get("Authorization") != "Bearer tok" || f.req.Method != http.MethodPost {
		t.Errorf("request %v", f.req.Header)
	}
	for name, body := range map[string]string{"no editions": `{"data":{"editions":[]}}`, "no rating": `{"data":{"editions":[{"book":{"slug":"x"}}]}}`} {
		s = &Service{http: &fake{status: 200, body: body}, url: endpoint}
		if _, _, _, err := s.Fetch(context.Background(), "x"); !errors.Is(err, ErrNoData) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, f := range map[string]*fake{"500": {status: 500}, "graphql error": {status: 200, body: `{"errors":[{"message":"x"}]}`}, "garbage": {status: 200, body: `<html>`}} {
		if _, _, _, err := (&Service{http: f, url: endpoint}).Fetch(context.Background(), "x"); err == nil || errors.Is(err, ErrNoData) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

type multi struct {
	status map[string]int // isbn -> status override
	calls  []string
}

func (m *multi) Do(r *http.Request) (*safehttp.Response, error) {
	b, _ := io.ReadAll(r.Body)
	var in struct{ Variables struct{ Isbn string } }
	_ = json.Unmarshal(b, &in)
	isbn := in.Variables.Isbn
	m.calls = append(m.calls, isbn)
	h := http.Header{}
	st := 200
	if v, ok := m.status[isbn]; ok {
		st = v
		h.Set("Retry-After", "120")
	}
	body := `{"data":{"editions":[{"book":{"slug":"b-` + isbn + `","rating":4.0,"ratings_count":10}}]}}`
	if isbn == "9780000000002" {
		body = `{"data":{"editions":[]}}`
	}
	return &safehttp.Response{Status: st, Header: h, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func seedWork(t *testing.T, pool *pgxpool.Pool, isbn string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var w uuid.UUID
	if err := pool.QueryRow(ctx, "INSERT INTO works (title) VALUES ('t') RETURNING id").Scan(&w); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO editions (work_id, isbn13, title) VALUES ($1, $2, 't')", w, isbn); err != nil {
		t.Fatal(err)
	}
	return w
}

func TestSyncRateLimitRetryAndStaleness(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	w1 := seedWork(t, pool, "9780306406157")
	w2 := seedWork(t, pool, "9780000000002") // Hardcover has no rating
	w3 := seedWork(t, pool, "9782070612758")
	m := &multi{status: map[string]int{}}
	s := New(pool, m, "tok")
	var sleeps []time.Duration
	s.sleep = func(_ context.Context, d time.Duration) error { sleeps = append(sleeps, d); return nil }

	n, after, err := s.Sync(ctx, 10)
	if err != nil || after != 0 || n != 3 {
		t.Fatalf("first run: %d %v %v", n, after, err)
	}
	if len(sleeps) != 2 || sleeps[0] != s.Interval {
		t.Errorf("requests must be spaced by the rate limit: %v", sleeps)
	}
	var cnt int
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM external_grades").Scan(&cnt)
	if cnt != 2 {
		t.Errorf("ratings stored: %d", cnt)
	}
	// nothing is due right after
	m.calls = nil
	if n, _, _ := s.Sync(ctx, 10); n != 0 || len(m.calls) != 0 {
		t.Errorf("second run must do nothing: %d %v", n, m.calls)
	}
	// age one rated and the unrated one: rated after 3 days, unrated only after 7
	_, _ = pool.Exec(ctx, "UPDATE external_grade_checks SET checked_at = now() - interval '4 days'")
	if n, _, _ := s.Sync(ctx, 10); n != 2 {
		t.Errorf("after 4 days only the 2 rated books are due, got %d", n)
	}
	_, _ = pool.Exec(ctx, "UPDATE external_grade_checks SET checked_at = now() - interval '8 days' WHERE work_id = $1", w2)
	m.calls = nil
	if n, _, _ := s.Sync(ctx, 10); n != 1 || m.calls[0] != "9780000000002" {
		t.Errorf("after 8 days the unrated book is rechecked: %d %v", n, m.calls)
	}

	// 429: stop, report the delay, and do not mark the book as checked
	_, _ = pool.Exec(ctx, "DELETE FROM external_grade_checks WHERE work_id = $1", w3)
	m.status["9782070612758"] = 429
	n, after, err = s.Sync(ctx, 10)
	if err != nil || after != 120*time.Second || n != 0 {
		t.Errorf("rate limited: %d %v %v", n, after, err)
	}
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM external_grade_checks WHERE work_id = $1", w3).Scan(&cnt)
	if cnt != 0 {
		t.Error("a rate-limited lookup must stay due")
	}
	// server errors are recorded with a growing delay and stop the run after three in a row
	m.status["9782070612758"] = 500
	_, _ = pool.Exec(ctx, "DELETE FROM external_grade_checks WHERE work_id = $1", w1)
	if _, _, err := s.Sync(ctx, 10); err != nil {
		t.Fatalf("one failure is not fatal: %v", err)
	}
	var status string
	var attempts int
	_ = pool.QueryRow(ctx, "SELECT status, attempts FROM external_grade_checks WHERE work_id = $1", w3).Scan(&status, &attempts)
	if status != "error" || attempts != 1 {
		t.Errorf("error check: %s %d", status, attempts)
	}
	if n, _, _ := s.Sync(ctx, 10); n != 0 {
		t.Errorf("a freshly failed book must wait (backoff): %d", n)
	}
}
