// Package jobs runs background work on a PostgreSQL queue (River): loan reminders,
// notification delivery and housekeeping. Jobs are safe to retry.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/catalog"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/i18n"
	"github.com/larueli/biblisann/internal/library"
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
	UserID uuid.UUID  `json:"user_id"`
	LoanID *uuid.UUID `json:"loan_id,omitempty"`
	Event  string     `json:"event"`
	Title  string     `json:"title"`
	Body   string     `json:"body"`
}

// Kind implements river.JobArgs.
func (NotifyArgs) Kind() string { return "notify" }

// HardcoverArgs refreshes ratings from Hardcover for the works that are due.
type HardcoverArgs struct{}

// Kind implements river.JobArgs.
func (HardcoverArgs) Kind() string { return "hardcover_sync" }

// Syncer refreshes external ratings within a request budget (see hardcover.Service.Sync).
type Syncer interface {
	Sync(ctx context.Context, budget int) (processed int, retryAfter time.Duration, err error)
}

// UserSyncArgs synchronizes the users whose Hardcover link is due.
type UserSyncArgs struct{}

// Kind implements river.JobArgs.
func (UserSyncArgs) Kind() string { return "hardcover_user_sync" }

// UserSyncer runs the per-user Hardcover synchronization (see hcsync.Service).
type UserSyncer interface {
	DueUsers(ctx context.Context, limit int) ([]uuid.UUID, error)
	SyncUserForJob(ctx context.Context, userID uuid.UUID) error
}

// MetadataRefreshArgs asks the providers again about books they did not know. Without fields it is
// the weekly run over the whole instance; administrators start narrower or wider runs on demand.
type MetadataRefreshArgs struct {
	LibraryID    *uuid.UUID `json:"library_id,omitempty"`
	IncludeKnown bool       `json:"include_known,omitempty"`
	IgnoreWait   bool       `json:"ignore_wait,omitempty"`
}

// Kind implements river.JobArgs.
func (MetadataRefreshArgs) Kind() string { return "metadata_refresh" }

// MetadataRefresher refreshes catalog editions in batches (catalog.Service).
type MetadataRefresher interface {
	RefreshBatch(ctx context.Context, sc catalog.RefreshScope, pause time.Duration) (catalog.RefreshReport, error)
}

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
func New(pool *pgxpool.Pool, d *notify.Dispatcher, log *slog.Logger, purge func(context.Context) error, hc Syncer, us UserSyncer, mr MetadataRefresher) (*Client, error) {
	workers := river.NewWorkers()
	q := dbsqlc.New(pool)
	access := library.NewService(pool)
	river.AddWorker(workers, &reminderWorker{pool: pool, q: q, access: access})
	river.AddWorker(workers, &notifyWorker{d: d, q: q, access: access})
	river.AddWorker(workers, &maintenanceWorker{purge: purge, q: q})
	periodic := []*river.PeriodicJob{
		river.NewPeriodicJob(river.PeriodicInterval(time.Hour), func() (river.JobArgs, *river.InsertOpts) {
			return ReminderArgs{}, nil
		}, &river.PeriodicJobOpts{RunOnStart: true}),
		river.NewPeriodicJob(river.PeriodicInterval(6*time.Hour), func() (river.JobArgs, *river.InsertOpts) {
			return MaintenanceArgs{}, nil
		}, nil),
	}
	if hc != nil {
		river.AddWorker(workers, &hardcoverWorker{s: hc, log: log})
		// Unique: a long run is never doubled by the next tick.
		periodic = append(periodic, river.NewPeriodicJob(river.PeriodicInterval(10*time.Minute), func() (river.JobArgs, *river.InsertOpts) {
			return HardcoverArgs{}, &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: 9 * time.Minute}}
		}, &river.PeriodicJobOpts{RunOnStart: true}))
	}
	if us != nil {
		river.AddWorker(workers, &userSyncWorker{s: us, log: log})
		periodic = append(periodic, river.NewPeriodicJob(river.PeriodicInterval(5*time.Minute), func() (river.JobArgs, *river.InsertOpts) {
			return UserSyncArgs{}, &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: 4 * time.Minute}}
		}, &river.PeriodicJobOpts{RunOnStart: true}))
	}
	if mr != nil {
		river.AddWorker(workers, &metadataRefreshWorker{r: mr, log: log})
		// Weekly: books no provider knew get another chance (a provider may have learned them since).
		periodic = append(periodic, river.NewPeriodicJob(river.PeriodicInterval(7*24*time.Hour), func() (river.JobArgs, *river.InsertOpts) {
			return MetadataRefreshArgs{}, &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: 6 * 24 * time.Hour}}
		}, nil))
	}
	c, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues:       map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 4}},
		Workers:      workers,
		Logger:       log,
		PeriodicJobs: periodic,
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

// EnqueueMetadataRefresh starts a metadata refresh in the background (administrator action). The
// same request twice within ten minutes is one job.
func (c *Client) EnqueueMetadataRefresh(ctx context.Context, a MetadataRefreshArgs) error {
	_, err := c.c.Insert(ctx, a, &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: 10 * time.Minute}})
	return err
}

// Notify enqueues a notification inside an existing transaction.
func (c *Client) Notify(ctx context.Context, tx pgx.Tx, a NotifyArgs) error {
	_, err := c.c.InsertTx(ctx, tx, a, nil)
	return err
}

type notifyWorker struct {
	river.WorkerDefaults[NotifyArgs]
	d      *notify.Dispatcher
	q      *dbsqlc.Queries
	access *library.Service
}

func (w *notifyWorker) Work(ctx context.Context, job *river.Job[NotifyArgs]) error {
	a := job.Args
	if strings.HasPrefix(a.Event, "loan.") {
		// Old queued notifications have no loan ID and cannot be re-authorized safely.
		if a.LoanID == nil {
			return nil
		}
		allowed, err := reminderAllowed(ctx, w.q, w.access, a.UserID, *a.LoanID)
		if err != nil || !allowed {
			return err
		}
	}
	return w.d.Deliver(ctx, a.UserID, notify.Message{Event: a.Event, Title: a.Title, Body: a.Body})
}

// reminderAllowed checks current account and loan permissions, including group/profile grants.
func reminderAllowed(ctx context.Context, q *dbsqlc.Queries, access *library.Service, userID, loanID uuid.UUID) (bool, error) {
	u, err := q.GetUser(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if u.DisabledAt != nil {
		return false, nil
	}
	l, err := q.GetLoan(ctx, loanID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if l.OwnerID != userID || l.ReturnedOn != nil {
		return false, nil
	}
	ctx = authz.With(ctx, &authz.Principal{UserID: userID})
	_, err = access.RequirePerm(ctx, q, l.LibraryID, authz.PermLoan)
	if errors.Is(err, authz.ErrForbidden) || errors.Is(err, authz.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
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
	pool   *pgxpool.Pool
	q      *dbsqlc.Queries
	access *library.Service
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
	allowed, err := reminderAllowed(ctx, q, w.access, l.OwnerID, l.ID)
	if err != nil || !allowed {
		return err
	}
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
		LoanID: &l.ID,
		Event:  "loan." + r.Kind,
		Title:  i18n.T(l.OwnerLocale, "loan."+r.Kind+".title", l.Title),
		Body:   i18n.T(l.OwnerLocale, bodyKey, l.Title, who, due),
	}
}

type hardcoverWorker struct {
	river.WorkerDefaults[HardcoverArgs]
	s   Syncer
	log *slog.Logger
}

// Timeout: a run makes up to 50 requests spaced by the rate limit.
func (w *hardcoverWorker) Timeout(*river.Job[HardcoverArgs]) time.Duration { return 10 * time.Minute }

func (w *hardcoverWorker) Work(ctx context.Context, _ *river.Job[HardcoverArgs]) error {
	n, after, err := w.s.Sync(ctx, 50)
	if err != nil {
		return err // River retries with backoff
	}
	if n > 0 {
		w.log.InfoContext(ctx, "hardcover ratings refreshed", "works", n)
	}
	if after > 0 {
		w.log.WarnContext(ctx, "hardcover asked to slow down", "retry_after", after.String())
		return river.JobSnooze(after)
	}
	return nil
}

type userSyncWorker struct {
	river.WorkerDefaults[UserSyncArgs]
	s   UserSyncer
	log *slog.Logger
}

func (w *userSyncWorker) Timeout(*river.Job[UserSyncArgs]) time.Duration { return 15 * time.Minute }

// Work syncs a few due users per run; a failing user is recorded on their own link and never
// stops the others.
func (w *userSyncWorker) Work(ctx context.Context, _ *river.Job[UserSyncArgs]) error {
	ids, err := w.s.DueUsers(ctx, 5)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := w.s.SyncUserForJob(ctx, id); err != nil {
			w.log.WarnContext(ctx, "hardcover user sync failed", "user", id.String(), "error", err)
		}
	}
	return nil
}

type metadataRefreshWorker struct {
	river.WorkerDefaults[MetadataRefreshArgs]
	r   MetadataRefresher
	log *slog.Logger
}

func (w *metadataRefreshWorker) Timeout(*river.Job[MetadataRefreshArgs]) time.Duration {
	return 30 * time.Minute
}

// Work refreshes up to a bounded number of editions, one lookup per second, so providers are not
// hammered; what is left waits for the next run.
func (w *metadataRefreshWorker) Work(ctx context.Context, job *river.Job[MetadataRefreshArgs]) error {
	a := job.Args
	rep, err := w.r.RefreshBatch(ctx, catalog.RefreshScope{LibraryID: a.LibraryID, IncludeKnown: a.IncludeKnown, IgnoreWait: a.IgnoreWait, Limit: 500}, time.Second)
	if err != nil {
		return err
	}
	w.log.InfoContext(ctx, "metadata refresh", "checked", rep.Checked, "updated", rep.Updated, "not_found", rep.NotFound, "providers_unavailable", rep.Stopped)
	return nil
}
