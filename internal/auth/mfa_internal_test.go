package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
)

type failingMFAQuery struct{ err error }
type failingMFARow struct{ err error }

func (r failingMFARow) Scan(...any) error { return r.err }
func (q failingMFAQuery) QueryRow(context.Context, string, ...any) pgx.Row {
	return failingMFARow(q)
}
func (q failingMFAQuery) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, q.err }
func (q failingMFAQuery) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, q.err
}

func TestMFALookupFailsClosed(t *testing.T) {
	failure := errors.New("database unavailable")
	for _, tc := range []struct {
		name   string
		lookup error
		want   error
	}{
		{"no enrollment", pgx.ErrNoRows, nil},
		{"database failure", failure, failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{q: dbsqlc.New(failingMFAQuery{tc.lookup})}
			enabled, err := s.IsMFAEnabled(context.Background(), uuid.New())
			if enabled || !errors.Is(err, tc.want) {
				t.Fatalf("enabled=%v, error=%v", enabled, err)
			}
		})
	}
}
