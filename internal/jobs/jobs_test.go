package jobs_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/larueli/biblisann/internal/auth"
	"github.com/larueli/biblisann/internal/authz"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/dbtest"
	"github.com/larueli/biblisann/internal/jobs"
	"github.com/larueli/biblisann/internal/library"
	"github.com/larueli/biblisann/internal/loans"
	"github.com/larueli/biblisann/internal/notify"
)

func TestClassify(t *testing.T) {
	today := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	day := func(n int) time.Time { return today.AddDate(0, 0, n) }
	tests := []struct {
		due  time.Time
		want jobs.Reminder
		ok   bool
	}{
		{day(2), jobs.Reminder{}, false},
		{day(1), jobs.Reminder{Kind: "due_soon"}, true},
		{day(0), jobs.Reminder{Kind: "due_today"}, true},
		{day(-1), jobs.Reminder{Kind: "overdue", Bucket: 0}, true},
		{day(-7), jobs.Reminder{Kind: "overdue", Bucket: 0}, true},
		{day(-8), jobs.Reminder{Kind: "overdue", Bucket: 1}, true},
		{day(-15), jobs.Reminder{Kind: "overdue", Bucket: 2}, true},
	}
	for _, tt := range tests {
		got, ok := jobs.Classify(today, tt.due)
		if ok != tt.ok || got != tt.want {
			t.Errorf("due %v: %+v %v, want %+v %v", tt.due.Format("01-02"), got, ok, tt.want, tt.ok)
		}
	}
}

type capture struct {
	mu   sync.Mutex
	msgs []notify.Message
}

func (c *capture) Kind() string { return "webhook" }
func (c *capture) Send(_ context.Context, _ dbsqlc.NotificationChannel, m notify.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, m)
	return nil
}
func (c *capture) all() []notify.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]notify.Message(nil), c.msgs...)
}

func TestReminderEndToEnd(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	if err := jobs.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := jobs.Migrate(ctx, pool); err != nil {
		t.Fatalf("second River migration must be a no-op: %v", err)
	}
	a, _ := auth.NewService(pool, auth.RegistrationOpen)
	u, _ := a.Register(ctx, "a@example.org", "tulips in the garden", "A")
	uctx := authz.With(ctx, &authz.Principal{UserID: u.ID, Via: "session"})
	lib := library.NewService(pool)
	l, _ := lib.CreateLibrary(uctx, "Home")
	ln := loans.NewService(pool, lib)
	due := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -3)
	if _, err := ln.Borrow(uctx, loans.BorrowInput{LibraryID: l.ID, Title: "Dune", Counterparty: "City library", DueOn: &due}); err != nil {
		t.Fatal(err)
	}
	q := dbsqlc.New(pool)
	if _, err := q.CreateChannel(ctx, dbsqlc.CreateChannelParams{UserID: u.ID, Kind: "webhook", Target: "https://hooks.example/x", Config: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}

	cap := &capture{}
	d := notify.NewDispatcher(q, slog.New(slog.NewTextHandler(io.Discard, nil)), cap)
	c, err := jobs.New(pool, d, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := c.Start(runCtx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Stop(context.Background()) }()

	deadline := time.Now().Add(20 * time.Second)
	for len(cap.all()) == 0 && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
	}
	msgs := cap.all()
	if len(msgs) != 1 || msgs[0].Event != "loan.overdue" || msgs[0].Title != "Overdue: Dune" {
		t.Fatalf("messages: %+v", msgs)
	}
	// A second scan must not notify again (same overdue bucket).
	if _, err := pool.Exec(ctx, "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM loan_notifications").Scan(&n)
	if n != 1 {
		t.Errorf("notification marks: %d", n)
	}
}
