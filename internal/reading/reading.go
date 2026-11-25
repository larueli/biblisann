// Package reading holds per-user reading status, local grades and comments, and statistics.
// External (critic or provider) grades are stored and shown separately.
package reading

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/larueli/biblisann/internal/authz"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
)

// ErrInvalid is returned for invalid input.
var ErrInvalid = errors.New("invalid input")

// ErrNotFound is returned when the work or record does not exist.
var ErrNotFound = authz.ErrNotFound

// Service is the reading service.
type Service struct {
	pool interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	}
	q *dbsqlc.Queries
}

// NewService creates the service.
func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool, q: dbsqlc.New(pool)} }

// WithTx returns a transaction-bound reading service.
func (s *Service) WithTx(tx pgx.Tx) *Service {
	copy := *s
	copy.pool, copy.q = tx, s.q.WithTx(tx)
	return &copy
}

var statuses = map[string]bool{"to_read": true, "reading": true, "read": true, "abandoned": true}

func (s *Service) principal(ctx context.Context, write bool) (*authz.Principal, error) {
	p, err := authz.From(ctx)
	if err != nil {
		return nil, err
	}
	scope := "read"
	if write {
		scope = "write"
	}
	if !p.HasScope(scope) && (write || !p.HasScope("write")) {
		return nil, authz.ErrForbidden
	}
	return p, nil
}

func (s *Service) workExists(ctx context.Context, id uuid.UUID) error {
	var n int
	if err := s.pool.QueryRow(ctx, "SELECT 1 FROM works WHERE id = $1", id).Scan(&n); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	return nil
}

// SetStatus records the reading status. Dates are optional; "reading" gets today as start date
// and "read" today as finish date when none is given.
func (s *Service) SetStatus(ctx context.Context, workID uuid.UUID, status string, started, finished *time.Time) (dbsqlc.ReadingState, error) {
	p, err := s.principal(ctx, true)
	if err != nil {
		return dbsqlc.ReadingState{}, err
	}
	if !statuses[status] {
		return dbsqlc.ReadingState{}, ErrInvalid
	}
	if err := s.workExists(ctx, workID); err != nil {
		return dbsqlc.ReadingState{}, err
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if status == "reading" && started == nil {
		started = &today
	}
	if status == "read" && finished == nil {
		finished = &today
	}
	if started != nil && finished != nil && finished.Before(*started) {
		return dbsqlc.ReadingState{}, ErrInvalid
	}
	return s.q.UpsertReadingState(ctx, dbsqlc.UpsertReadingStateParams{UserID: p.UserID, WorkID: workID, Status: status,
		StartedOn: started, FinishedOn: finished})
}

// ClearStatus removes the reading status of a work.
func (s *Service) ClearStatus(ctx context.Context, workID uuid.UUID) error {
	p, err := s.principal(ctx, true)
	if err != nil {
		return err
	}
	return s.q.DeleteReadingState(ctx, dbsqlc.DeleteReadingStateParams{UserID: p.UserID, WorkID: workID})
}

// WorkState is everything a user knows about a work.
type WorkState struct {
	Reading  *dbsqlc.ReadingState
	Grade    *dbsqlc.Grade
	External []dbsqlc.ExternalGrade
}

// Get returns the caller's status and grade of a work, plus external grades.
func (s *Service) Get(ctx context.Context, workID uuid.UUID) (WorkState, error) {
	p, err := s.principal(ctx, false)
	if err != nil {
		return WorkState{}, err
	}
	if err := s.workExists(ctx, workID); err != nil {
		return WorkState{}, err
	}
	var ws WorkState
	if r, err := s.q.GetReadingState(ctx, dbsqlc.GetReadingStateParams{UserID: p.UserID, WorkID: workID}); err == nil {
		ws.Reading = &r
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return ws, err
	}
	if g, err := s.q.GetGrade(ctx, dbsqlc.GetGradeParams{UserID: p.UserID, WorkID: workID}); err == nil {
		ws.Grade = &g
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return ws, err
	}
	ws.External, err = s.q.ListExternalGrades(ctx, workID)
	return ws, err
}

// SetGrade stores the caller's grade (1 to 10, or nil for a comment only) and comment.
func (s *Service) SetGrade(ctx context.Context, workID uuid.UUID, grade *int, comment string) (dbsqlc.Grade, error) {
	p, err := s.principal(ctx, true)
	if err != nil {
		return dbsqlc.Grade{}, err
	}
	if (grade != nil && (*grade < 1 || *grade > 10)) || len(comment) > 10000 || (grade == nil && strings.TrimSpace(comment) == "") {
		return dbsqlc.Grade{}, ErrInvalid
	}
	if err := s.workExists(ctx, workID); err != nil {
		return dbsqlc.Grade{}, err
	}
	var g *int16
	if grade != nil {
		v := int16(*grade)
		g = &v
	}
	return s.q.UpsertGrade(ctx, dbsqlc.UpsertGradeParams{UserID: p.UserID, WorkID: workID, Grade: g, Comment: comment})
}

// DeleteGrade removes the caller's grade and comment.
func (s *Service) DeleteGrade(ctx context.Context, workID uuid.UUID) error {
	p, err := s.principal(ctx, true)
	if err != nil {
		return err
	}
	return s.q.DeleteGrade(ctx, dbsqlc.DeleteGradeParams{UserID: p.UserID, WorkID: workID})
}

// ListByStatus lists the caller's works with a status (empty status: all).
func (s *Service) ListByStatus(ctx context.Context, status string, limit int) ([]dbsqlc.ListReadingStatesRow, error) {
	p, err := s.principal(ctx, false)
	if err != nil {
		return nil, err
	}
	if status != "" && !statuses[status] {
		return nil, ErrInvalid
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return s.q.ListReadingStates(ctx, dbsqlc.ListReadingStatesParams{UserID: p.UserID, Column2: status, Limit: int32(limit)})
}

// Stats summarizes the caller's reading.
type Stats struct {
	ByStatus     map[string]int
	ReadPerYear  map[int]int
	AverageGrade float64
	Graded       int
}

// Stats computes overall reading statistics.
func (s *Service) Stats(ctx context.Context) (Stats, error) {
	p, err := s.principal(ctx, false)
	if err != nil {
		return Stats{}, err
	}
	st := Stats{ByStatus: map[string]int{}, ReadPerYear: map[int]int{}}
	counts, err := s.q.ReadingCounts(ctx, p.UserID)
	if err != nil {
		return st, fmt.Errorf("reading: counts: %w", err)
	}
	for _, c := range counts {
		st.ByStatus[c.Status] = int(c.N)
	}
	years, err := s.q.ReadPerYear(ctx, p.UserID)
	if err != nil {
		return st, err
	}
	for _, y := range years {
		st.ReadPerYear[int(y.Year)] = int(y.N)
	}
	avg, err := s.q.AverageGrade(ctx, p.UserID)
	if err != nil {
		return st, err
	}
	st.AverageGrade, st.Graded = avg.Average, int(avg.Graded)
	return st, nil
}
