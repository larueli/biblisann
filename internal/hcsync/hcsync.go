// Package hcsync keeps a user's reading status and ratings in step with their own Hardcover
// account, in the direction(s) they chose (pull, push or both).
//
// What counts as "changed" is decided against a snapshot of what both sides agreed on at the last
// sync. A side that changed alone wins; when both changed to different values, the more recent
// edit wins. Only status and rating are synchronized, matched through ISBN. Requests are spaced to
// respect Hardcover's rate limit and each run has a request budget; unfinished work resumes at the
// next run. The Hardcover query shapes follow its public schema but were only exercised against
// recorded responses (see internal/hardcover).
package hcsync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/larueli/biblisann/internal/authz"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/hardcover"
	"github.com/larueli/biblisann/internal/reading"
	"github.com/larueli/biblisann/internal/secretbox"
)

// Remote is the part of Hardcover the engine uses (implemented by hardcover.UserClient).
type Remote interface {
	Me(ctx context.Context) (int64, error)
	UserBooks(ctx context.Context, offset, limit int) ([]hardcover.RemoteBook, error)
	BookIDByISBN(ctx context.Context, isbn13 string) (int64, error)
	Upsert(ctx context.Context, userBookID, bookID int64, statusID int, rating *float64) (int64, error)
}

// RemoteFactory builds a Remote from a user's token.
type RemoteFactory func(token string) Remote

// Errors.
var (
	ErrDisabled = errors.New("hardcover sync needs BIBLISANN_SECRET_KEY to store tokens")
	ErrMode     = errors.New("mode must be pull, push or both")
)

const (
	pageSize       = 100
	fullSyncEvery  = 6 * time.Hour
	resumeAfter    = 2 * time.Minute
	failureBackoff = 30 * time.Minute
	tokenRejected  = 24 * time.Hour
)

// Service runs the synchronization.
type Service struct {
	pool    *pgxpool.Pool
	q       *dbsqlc.Queries
	key     []byte
	remote  RemoteFactory
	reading *reading.Service
	// Interval is the delay between two Hardcover requests (rate limit); Budget bounds the requests of
	// one run. Sleep is replaceable in tests.
	Interval time.Duration
	Budget   int
	Sleep    func(context.Context, time.Duration) error
	now      func() time.Time
}

// New creates the service. key is the 32-byte instance key; without it Hardcover sync is off.
func New(pool *pgxpool.Pool, key []byte, f RemoteFactory, r *reading.Service) *Service {
	return &Service{pool: pool, q: dbsqlc.New(pool), key: key, remote: f, reading: r, Interval: 1200 * time.Millisecond, Budget: 40,
		Sleep: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		}, now: time.Now}
}

// Available reports whether tokens can be stored.
func (s *Service) Available() bool { return len(s.key) == secretbox.KeySize }

// Status describes a user's link.
type Status struct {
	Linked     bool
	Mode       string
	LastSyncAt *time.Time
	LastError  string
}

// Get returns the user's link status.
func (s *Service) Get(ctx context.Context, userID uuid.UUID) (Status, error) {
	l, err := s.q.GetHardcoverLink(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, err
	}
	return Status{Linked: true, Mode: l.Mode, LastSyncAt: l.LastSyncAt, LastError: l.LastError}, nil
}

// Link verifies the token with Hardcover and stores it (sealed) with the chosen mode.
func (s *Service) Link(ctx context.Context, userID uuid.UUID, token, mode string) error {
	if !s.Available() {
		return ErrDisabled
	}
	if mode != "pull" && mode != "push" && mode != "both" {
		return ErrMode
	}
	if token == "" {
		return errors.New("a Hardcover API token is required")
	}
	if _, err := s.remote(token).Me(ctx); err != nil {
		return err
	}
	sealed, err := secretbox.Seal(s.key, []byte(token))
	if err != nil {
		return err
	}
	return s.q.UpsertHardcoverLink(ctx, dbsqlc.UpsertHardcoverLinkParams{UserID: userID, TokenEnc: sealed, Mode: mode})
}

// SetMode changes the direction without touching the token.
func (s *Service) SetMode(ctx context.Context, userID uuid.UUID, mode string) error {
	if mode != "pull" && mode != "push" && mode != "both" {
		return ErrMode
	}
	if _, err := s.q.GetHardcoverLink(ctx, userID); err != nil {
		return err
	}
	return s.q.SetHardcoverMode(ctx, dbsqlc.SetHardcoverModeParams{UserID: userID, Mode: mode})
}

// Unlink forgets the token and the sync state. Local data and Hardcover data are left as they are.
func (s *Service) Unlink(ctx context.Context, userID uuid.UUID) error {
	if err := s.q.DeleteHardcoverState(ctx, userID); err != nil {
		return err
	}
	return s.q.DeleteHardcoverLink(ctx, userID)
}

// Mapping between Hardcover status IDs and local statuses.
var (
	toHC   = map[string]int{"to_read": 1, "reading": 2, "read": 3, "abandoned": 5}
	fromHC = map[int]string{1: "to_read", 2: "reading", 3: "read", 4: "reading", 5: "abandoned"} // 4 = paused
)

func ratingFromGrade(g *int16) *float64 {
	if g == nil {
		return nil
	}
	r := float64(*g) / 2
	return &r
}

func gradeFromRating(r *float64) *int16 {
	if r == nil || *r <= 0 {
		return nil
	}
	g := int16(*r*2 + 0.5)
	g = max(1, min(g, 10))
	return &g
}

func sameGrade(a, b *int16) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

// Result summarizes one run.
type Result struct {
	Pulled, Pushed int
	More           bool
}

type runner struct {
	s      *Service
	ctx    context.Context
	uctx   context.Context
	userID uuid.UUID
	remote Remote
	calls  int
}

// call spaces and counts Hardcover requests; false means the budget is spent.
func (r *runner) call() (bool, error) {
	if r.calls >= r.s.Budget {
		return false, nil
	}
	if r.calls > 0 {
		if err := r.s.Sleep(r.ctx, r.s.Interval); err != nil {
			return false, err
		}
	}
	r.calls++
	return true, nil
}

// SyncUser runs one budgeted synchronization for a user and schedules the next one.
func (s *Service) SyncUser(ctx context.Context, userID uuid.UUID) (Result, error) {
	link, err := s.q.GetHardcoverLink(ctx, userID)
	if err != nil {
		return Result{}, err
	}
	res, err := s.run(ctx, userID, link)
	next, msg := s.now().Add(fullSyncEvery), ""
	offset := int(link.PullOffset)
	if res.nextOffset >= 0 {
		offset = res.nextOffset
	}
	switch {
	case err == nil && res.More:
		next = s.now().Add(resumeAfter)
	case err == nil:
	case errors.Is(err, hardcover.ErrUnauthorized):
		next, msg = s.now().Add(tokenRejected), "Hardcover rejected the token: link your account again"
	default:
		var rl *hardcover.RateLimitedError
		if errors.As(err, &rl) {
			next, msg = s.now().Add(rl.After), ""
			err = nil
		} else {
			next, msg = s.now().Add(failureBackoff), "the last synchronization failed; it will be retried"
		}
	}
	if ferr := s.q.FinishHardcoverSync(ctx, dbsqlc.FinishHardcoverSyncParams{UserID: userID, PullOffset: int32(offset), NextSyncAt: next, LastError: msg}); ferr != nil {
		return res.Result, ferr
	}
	return res.Result, err
}

type runResult struct {
	Result
	nextOffset int
}

func (s *Service) run(ctx context.Context, userID uuid.UUID, link dbsqlc.HardcoverLink) (runResult, error) {
	res := runResult{nextOffset: -1}
	token, err := secretbox.Open(s.key, link.TokenEnc)
	if err != nil {
		return res, fmt.Errorf("hcsync: cannot open the stored token: %w", err)
	}
	r := &runner{s: s, ctx: ctx, userID: userID, remote: s.remote(string(token)),
		uctx: authz.With(ctx, &authz.Principal{UserID: userID, Via: "session"})}
	snaps, err := s.q.HardcoverBookState(ctx, userID)
	if err != nil {
		return res, err
	}
	state := map[uuid.UUID]dbsqlc.HardcoverBookState{}
	for _, sn := range snaps {
		state[sn.WorkID] = sn
	}
	remoteWon := map[uuid.UUID]bool{}

	if link.Mode == "pull" || link.Mode == "both" {
		off, more, err := r.pull(int(link.PullOffset), state, remoteWon, &res.Pulled)
		res.nextOffset = off
		res.More = res.More || more
		if err != nil {
			return res, err
		}
	}
	if link.Mode == "push" || link.Mode == "both" {
		more, err := r.push(state, remoteWon, &res.Pushed)
		res.More = res.More || more
		if err != nil {
			return res, err
		}
	}
	return res, nil
}

// pull reads pages of the remote library until the budget is spent or the end is reached.
func (r *runner) pull(offset int, state map[uuid.UUID]dbsqlc.HardcoverBookState, remoteWon map[uuid.UUID]bool, count *int) (next int, more bool, err error) {
	local, err := r.localByWork()
	if err != nil {
		return offset, false, err
	}
	for {
		ok, err := r.call()
		if err != nil {
			return offset, false, err
		}
		if !ok {
			return offset, true, nil
		}
		page, err := r.remote.UserBooks(r.ctx, offset, pageSize)
		if err != nil {
			return offset, false, err
		}
		for _, rb := range page {
			changed, err := r.reconcile(rb, local, state, remoteWon)
			if err != nil {
				return offset, false, err
			}
			if changed {
				*count++
			}
		}
		offset += len(page)
		if len(page) < pageSize {
			return 0, false, nil // reached the end: the next full pull starts over
		}
	}
}

func (r *runner) localByWork() (map[uuid.UUID]dbsqlc.LocalReadingForSyncRow, error) {
	rows, err := r.s.q.LocalReadingForSync(r.ctx, r.userID)
	if err != nil {
		return nil, err
	}
	m := make(map[uuid.UUID]dbsqlc.LocalReadingForSyncRow, len(rows))
	for _, row := range rows {
		m[row.WorkID] = row
	}
	return m, nil
}

// reconcile applies one remote entry to the local side when it is the only or the latest change.
func (r *runner) reconcile(rb hardcover.RemoteBook, local map[uuid.UUID]dbsqlc.LocalReadingForSyncRow,
	state map[uuid.UUID]dbsqlc.HardcoverBookState, remoteWon map[uuid.UUID]bool) (bool, error) {
	status, known := fromHC[rb.StatusID]
	if !known {
		return false, nil
	}
	var work uuid.UUID
	for _, isbn := range rb.ISBNs {
		if w, err := r.s.q.WorkIDByISBN(r.ctx, &isbn); err == nil {
			work = w
			break
		}
	}
	if work == uuid.Nil {
		return false, nil // a book this instance does not know: pulling never invents catalog entries
	}
	rGrade := gradeFromRating(rb.Rating)
	snap, haveSnap := state[work]
	cur, haveLocal := local[work]

	remoteChanged := !haveSnap || snap.Status != status || !sameGrade(snap.Grade, rGrade)
	localChanged := false
	if haveLocal {
		localChanged = !haveSnap || cur.Status != snap.Status || !sameGrade(cur.Grade, snap.Grade)
	}
	if !remoteChanged {
		return false, nil
	}
	if haveLocal && cur.Status == status && sameGrade(cur.Grade, rGrade) { // already equal
		return false, r.remember(work, rb, status, rGrade)
	}
	if localChanged && haveLocal {
		localAt := cur.StatusUpdatedAt
		if cur.GradeUpdatedAt != nil && cur.GradeUpdatedAt.After(localAt) {
			localAt = *cur.GradeUpdatedAt
		}
		if !rb.UpdatedAt.After(localAt) {
			return false, nil // the local edit is newer: the push phase sends it
		}
	}
	if _, err := r.s.reading.SetStatus(r.uctx, work, status, nil, nil); err != nil {
		return false, err
	}
	if rGrade != nil {
		g := int(*rGrade)
		if _, err := r.s.reading.SetGrade(r.uctx, work, &g, ""); err != nil {
			return false, err
		}
	}
	remoteWon[work] = true
	return true, r.remember(work, rb, status, rGrade)
}

func (r *runner) remember(work uuid.UUID, rb hardcover.RemoteBook, status string, grade *int16) error {
	return r.s.q.UpsertHardcoverBookState(r.ctx, dbsqlc.UpsertHardcoverBookStateParams{UserID: r.userID, WorkID: work,
		HcBookID: rb.BookID, HcUserBookID: &rb.UserBookID, Status: status, Grade: grade})
}

// push sends local changes (new or different from the snapshot) to Hardcover.
func (r *runner) push(state map[uuid.UUID]dbsqlc.HardcoverBookState, remoteWon map[uuid.UUID]bool, count *int) (more bool, err error) {
	local, err := r.localByWork()
	if err != nil {
		return false, err
	}
	for work, cur := range local {
		snap, haveSnap := state[work]
		if remoteWon[work] || (haveSnap && snap.Status == cur.Status && sameGrade(snap.Grade, cur.Grade)) {
			continue
		}
		statusID, ok := toHC[cur.Status]
		if !ok || cur.Isbn13 == "" {
			continue
		}
		bookID, userBookID := snap.HcBookID, int64(0)
		if haveSnap && snap.HcUserBookID != nil {
			userBookID = *snap.HcUserBookID
		}
		if !haveSnap {
			okc, err := r.call()
			if err != nil {
				return false, err
			}
			if !okc {
				return true, nil
			}
			if bookID, err = r.remote.BookIDByISBN(r.ctx, cur.Isbn13); err != nil {
				return false, err
			}
			if bookID == 0 {
				continue // Hardcover does not know this book
			}
		}
		okc, err := r.call()
		if err != nil {
			return false, err
		}
		if !okc {
			return true, nil
		}
		id, err := r.remote.Upsert(r.ctx, userBookID, bookID, statusID, ratingFromGrade(cur.Grade))
		if err != nil {
			return false, err
		}
		if err := r.s.q.UpsertHardcoverBookState(r.ctx, dbsqlc.UpsertHardcoverBookStateParams{UserID: r.userID, WorkID: work,
			HcBookID: bookID, HcUserBookID: &id, Status: cur.Status, Grade: cur.Grade}); err != nil {
			return false, err
		}
		*count++
	}
	return false, nil
}

// DueUsers lists users whose next synchronization is due.
func (s *Service) DueUsers(ctx context.Context, limit int) ([]uuid.UUID, error) {
	return s.q.DueHardcoverLinks(ctx, int32(limit))
}

// SyncUserForJob runs SyncUser and ignores the outcome that is already recorded on the link.
func (s *Service) SyncUserForJob(ctx context.Context, userID uuid.UUID) error {
	_, err := s.SyncUser(ctx, userID)
	if errors.Is(err, hardcover.ErrUnauthorized) {
		return nil // the link shows "link your account again"
	}
	return err
}
