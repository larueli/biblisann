// Package jobs runs background work on a PostgreSQL queue (River): loan reminders,
// notification delivery and housekeeping. Jobs are safe to retry.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/i18n"
	"github.com/larueli/biblisann/internal/notify"
)

// Migrate applies River's own schema migrations. Call it after the application migrations.
// River serializes concurrent callers itself; the advisory lock only keeps replicas calm.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("jobs: acquire: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtext('biblisann.river.migrate'))"); err != nil {
		return fmt.Errorf("jobs: lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock(hashtext('biblisann.river.migrate'))")
	}()
	m, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("jobs: migrator: %w", err)
	}
	if _, err := m.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("jobs: migrate: %w", err)
	}
	return nil
}

// ReminderArgs triggers a scan of open loans.
type ReminderArgs struct{}

// Kind implements river.JobArgs.
func (ReminderArgs) Kind() string { return "loan_reminders" }

// NotifyArgs delivers one message to a user's channels.
type NotifyArgs struct {
	UserID uuid.UUID `json:"user_id"`
	Event  string    `json:"event"`
	Title  string    `json:"title"`
	Body   string    `json:"body"`
}

// Kind implements river.JobArgs.
func (NotifyArgs) Kind() string { return "notify" }

// MaintenanceArgs removes expired sessions and old idempotency keys.
type MaintenanceArgs struct{}

// Kind implements river.JobArgs.
func (MaintenanceArgs) Kind() string { return "maintenance" }

// Reminder classifies an open loan against today's date.
type Reminder struct {
	Kind   string // due_soon, due_today, overdue
	Bucket int    // overdue repeats every 7 days
}

// Classify returns the reminder due for a loan, if any.
func Classify(today, due time.Time) (Reminder, bool) {
	days := int(due.Sub(today).Hours() / 24)
	switch {
	case days == 1:
		return Reminder{"due_soon", 0}, true
	case days == 0:
		return Reminder{"due_today", 0}, true
	case days < 0:
		return Reminder{"overdue", (-days - 1) / 7}, true
	}
	return Reminder{}, false
}

// Client wraps the River client.
type Client struct {
	c *river.Client[pgx.Tx]
}

// New builds the client with its workers and periodic jobs. Call Start to begin processing.
func New(pool *pgxpool.Pool, d *notify.Dispatcher, log *slog.Logger, purge func(context.Context) error) (*Client, error) {
	workers := river.NewWorkers()
	q := dbsqlc.New(pool)
	river.AddWorker(workers, &reminderWorker{pool: pool, q: q})
	river.AddWorker(workers, &notifyWorker{d: d})
	river.AddWorker(workers, &maintenanceWorker{purge: purge, q: q})
	c, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues:  map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 4}},
		Workers: workers,
		Logger:  log,
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(river.PeriodicInterval(time.Hour), func() (river.JobArgs, *river.InsertOpts) {
				return ReminderArgs{}, nil
			}, &river.PeriodicJobOpts{RunOnStart: true}),
			river.NewPeriodicJob(river.PeriodicInterval(6*time.Hour), func() (river.JobArgs, *river.InsertOpts) {
				return MaintenanceArgs{}, nil
			}, nil),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("jobs: client: %w", err)
	}
	return &Client{c: c}, nil
}

// Start begins processing in the background.
func (c *Client) Start(ctx context.Context) error { return c.c.Start(ctx) }

// Stop waits for running jobs to finish (bounded by ctx).
func (c *Client) Stop(ctx context.Context) error { return c.c.Stop(ctx) }

// Notify enqueues a notification inside an existing transaction.
func (c *Client) Notify(ctx context.Context, tx pgx.Tx, a NotifyArgs) error {
	_, err := c.c.InsertTx(ctx, tx, a, nil)
	return err
}

type notifyWorker struct {
	river.WorkerDefaults[NotifyArgs]
	d *notify.Dispatcher
}

func (w *notifyWorker) Work(ctx context.Context, job *river.Job[NotifyArgs]) error {
	a := job.Args
	return w.d.Deliver(ctx, a.UserID, notify.Message{Event: a.Event, Title: a.Title, Body: a.Body})
}

type maintenanceWorker struct {
	river.WorkerDefaults[MaintenanceArgs]
	purge func(context.Context) error
	q     *dbsqlc.Queries
}

func (w *maintenanceWorker) Work(ctx context.Context, _ *river.Job[MaintenanceArgs]) error {
	if w.purge != nil {
		if err := w.purge(ctx); err != nil {
			return err
		}
	}
	_, err := w.q.DeleteOldIdempotency(ctx)
	return err
}

type reminderWorker struct {
	river.WorkerDefaults[ReminderArgs]
	pool *pgxpool.Pool
	q    *dbsqlc.Queries
}

func (w *reminderWorker) Work(ctx context.Context, _ *river.Job[ReminderArgs]) error {
	return w.Run(ctx, time.Now())
}

// Run enqueues reminders for loans due by tomorrow. Marking and enqueueing share a transaction,
// so a loan is never notified twice for the same reminder and never silently skipped.
func (w *reminderWorker) Run(ctx context.Context, now time.Time) error {
	today := now.UTC().Truncate(24 * time.Hour)
	loans, err := w.q.OpenLoansDueBy(ctx, &[]time.Time{today.AddDate(0, 0, 1)}[0])
	if err != nil {
		return fmt.Errorf("jobs: open loans: %w", err)
	}
	client := river.ClientFromContext[pgx.Tx](ctx)
	for _, l := range loans {
		if l.DueOn == nil {
			continue
		}
		r, ok := Classify(today, *l.DueOn)
		if !ok {
			continue
		}
		if err := w.notifyOnce(ctx, client, l, r); err != nil {
			return err
		}
	}
	return nil
}

func (w *reminderWorker) notifyOnce(ctx context.Context, client *river.Client[pgx.Tx], l dbsqlc.OpenLoansDueByRow, r Reminder) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := w.q.WithTx(tx)
	sent, err := q.LoanNotificationSent(ctx, dbsqlc.LoanNotificationSentParams{LoanID: l.ID, Kind: r.Kind, Bucket: int32(r.Bucket)})
	if err != nil || sent {
		return err
	}
	if err := q.MarkLoanNotified(ctx, dbsqlc.MarkLoanNotifiedParams{LoanID: l.ID, Kind: r.Kind, Bucket: int32(r.Bucket)}); err != nil {
		return err
	}
	if _, err := client.InsertTx(ctx, tx, Compose(l, r), nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Compose builds the notification for a loan reminder in the owner's language.
func Compose(l dbsqlc.OpenLoansDueByRow, r Reminder) NotifyArgs {
	who := l.Counterparty
	bodyKey := "loan.lent.body"
	if l.Direction == "borrowed" {
		bodyKey = "loan.borrowed.body"
	}
	due := l.DueOn.Format("2006-01-02")
	return NotifyArgs{
		UserID: l.OwnerID,
		Event:  "loan." + r.Kind,
		Title:  i18n.T(l.OwnerLocale, "loan."+r.Kind+".title", l.Title),
		Body:   i18n.T(l.OwnerLocale, bodyKey, l.Title, who, due),
	}
}
