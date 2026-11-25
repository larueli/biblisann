// Package lists implements personal and library lists, the default wishlist, and series
// bookkeeping (missing volumes).
package lists

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/larueli/biblisann/internal/authz"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
)

// Errors.
var (
	ErrInvalid  = errors.New("invalid input")
	ErrNotFound = authz.ErrNotFound
)

// Access is what lists need from the library service.
type Access interface {
	RequirePerm(ctx context.Context, q *dbsqlc.Queries, libraryID uuid.UUID, perm string) (*authz.Principal, error)
}

// Service is the lists service.
type Service struct {
	pool   *pgxpool.Pool
	q      *dbsqlc.Queries
	access Access
}

// NewService creates the service.
func NewService(pool *pgxpool.Pool, a Access) *Service {
	return &Service{pool: pool, q: dbsqlc.New(pool), access: a}
}

// WithTx returns a transaction-bound service using the provided access checker.
func (s *Service) WithTx(tx pgx.Tx, access Access) *Service {
	copy := *s
	copy.q, copy.access = s.q.WithTx(tx), access
	return &copy
}

// authorize loads a list and checks the caller may read (or write) it.
func (s *Service) authorize(ctx context.Context, id uuid.UUID, write bool) (dbsqlc.List, *authz.Principal, error) {
	l, err := s.q.GetList(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, nil, ErrNotFound
	}
	if err != nil {
		return l, nil, err
	}
	if l.OwnerID != nil {
		p, err := authz.From(ctx)
		if err != nil {
			return l, nil, err
		}
		scope := "read"
		if write {
			scope = "write"
		}
		if *l.OwnerID != p.UserID || (!p.HasScope(scope) && (write || !p.HasScope("write"))) {
			return l, nil, ErrNotFound
		}
		return l, p, nil
	}
	perm := authz.PermRead
	if write {
		perm = authz.PermList
	}
	p, err := s.access.RequirePerm(ctx, nil, *l.LibraryID, perm)
	return l, p, err
}

// Wishlist returns (creating it on first use) the caller's default wishlist.
func (s *Service) Wishlist(ctx context.Context) (dbsqlc.List, error) {
	p, err := authz.From(ctx)
	if err != nil {
		return dbsqlc.List{}, err
	}
	if l, err := s.q.GetWishlist(ctx, &p.UserID); err == nil {
		return l, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return l, err
	}
	l, err := s.q.CreateList(ctx, dbsqlc.CreateListParams{OwnerID: &p.UserID, Name: "Wishlist", Kind: "wishlist"})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // created concurrently
		return s.q.GetWishlist(ctx, &p.UserID)
	}
	return l, err
}

// Create makes a personal list, or a library list when libraryID is set.
func (s *Service) Create(ctx context.Context, name string, libraryID *uuid.UUID) (dbsqlc.List, error) {
	p, err := authz.From(ctx)
	if err != nil {
		return dbsqlc.List{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 || !p.HasScope("write") {
		return dbsqlc.List{}, ErrInvalid
	}
	if libraryID != nil {
		if _, err := s.access.RequirePerm(ctx, nil, *libraryID, authz.PermList); err != nil {
			return dbsqlc.List{}, err
		}
		return s.q.CreateList(ctx, dbsqlc.CreateListParams{LibraryID: libraryID, Name: name, Kind: "custom"})
	}
	return s.q.CreateList(ctx, dbsqlc.CreateListParams{OwnerID: &p.UserID, Name: name, Kind: "custom"})
}

// All lists personal lists and the lists of libraries the caller can read.
func (s *Service) All(ctx context.Context) ([]dbsqlc.List, error) {
	p, err := authz.From(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.Wishlist(ctx); err != nil {
		return nil, err
	}
	rows, err := s.q.ListListsForUser(ctx, &p.UserID)
	if err != nil {
		return rows, err
	}
	out := rows[:0]
	readable := make(map[uuid.UUID]bool)
	checked := make(map[uuid.UUID]bool)
	for _, l := range rows {
		if l.LibraryID == nil {
			out = append(out, l)
			continue
		}
		id := *l.LibraryID
		if !checked[id] {
			_, accessErr := s.access.RequirePerm(ctx, nil, id, authz.PermRead)
			if accessErr != nil && !errors.Is(accessErr, authz.ErrForbidden) && !errors.Is(accessErr, authz.ErrNotFound) {
				return nil, accessErr
			}
			readable[id], checked[id] = accessErr == nil, true
		}
		if readable[id] {
			out = append(out, l)
		}
	}
	return out, nil
}

// Delete removes a custom list (the wishlist cannot be deleted).
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	if _, _, err := s.authorize(ctx, id, true); err != nil {
		return err
	}
	n, err := s.q.DeleteList(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrInvalid
	}
	return nil
}

// ItemInput describes a list item. With EditionID the edition's data is used.
type ItemInput struct {
	EditionID      *uuid.UUID
	Title          string
	Authors        []string
	ISBN13         string
	SeriesName     string
	SeriesPosition *float64
	Note           string
}

// AddItem adds a book to a list. The book does not have to be in any library.
func (s *Service) AddItem(ctx context.Context, listID uuid.UUID, in ItemInput) (dbsqlc.ListItem, error) {
	if _, _, err := s.authorize(ctx, listID, true); err != nil {
		return dbsqlc.ListItem{}, err
	}
	var isbn *string
	if in.EditionID != nil {
		e, err := s.q.GetEdition(ctx, *in.EditionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return dbsqlc.ListItem{}, ErrInvalid
		}
		if err != nil {
			return dbsqlc.ListItem{}, err
		}
		in.Title, in.Authors, isbn = e.Title, e.Authors, e.Isbn13
		in.SeriesName, in.SeriesPosition = e.SeriesName, e.SeriesPosition
	} else if in.ISBN13 != "" {
		isbn = &in.ISBN13
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" || len(in.Title) > 1000 || len(in.Note) > 2000 || len(in.Authors) > 50 {
		return dbsqlc.ListItem{}, ErrInvalid
	}
	if in.Authors == nil {
		in.Authors = []string{}
	}
	return s.q.AddListItem(ctx, dbsqlc.AddListItemParams{ListID: listID, EditionID: in.EditionID, Title: in.Title,
		Authors: in.Authors, Isbn13: isbn, SeriesName: in.SeriesName, SeriesPosition: in.SeriesPosition, Note: in.Note})
}

// Items lists a list's items with the in_library flag.
func (s *Service) Items(ctx context.Context, listID uuid.UUID) ([]dbsqlc.ListListItemsRow, error) {
	_, p, err := s.authorize(ctx, listID, false)
	if err != nil {
		return nil, err
	}
	libs, err := s.q.ListLibrariesForUser(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	readable := make([]uuid.UUID, 0, len(libs))
	for _, lib := range libs {
		_, accessErr := s.access.RequirePerm(ctx, nil, lib.ID, authz.PermRead)
		if errors.Is(accessErr, authz.ErrForbidden) || errors.Is(accessErr, authz.ErrNotFound) {
			continue
		}
		if accessErr != nil {
			return nil, accessErr
		}
		readable = append(readable, lib.ID)
	}
	return s.q.ListListItems(ctx, dbsqlc.ListListItemsParams{ListID: listID, ReadableLibraries: readable})
}

// RemoveItem deletes an item.
func (s *Service) RemoveItem(ctx context.Context, itemID uuid.UUID) error {
	it, err := s.q.GetListItem(ctx, itemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, _, err := s.authorize(ctx, it.ListID, true); err != nil {
		return err
	}
	return s.q.DeleteListItem(ctx, itemID)
}

// SeriesStatus describes one series of a library.
type SeriesStatus struct {
	Name    string
	Owned   []float64
	Missing []int // whole-number volumes between 1 and the highest owned that are not owned
}

// Series reports owned and missing volumes per series.
func (s *Service) Series(ctx context.Context, libraryID uuid.UUID) ([]SeriesStatus, error) {
	if _, err := s.access.RequirePerm(ctx, nil, libraryID, authz.PermRead); err != nil {
		return nil, err
	}
	rows, err := s.q.SeriesInLibrary(ctx, libraryID)
	if err != nil {
		return nil, fmt.Errorf("lists: series: %w", err)
	}
	out := make([]SeriesStatus, 0, len(rows))
	for _, r := range rows {
		st := SeriesStatus{Name: r.SeriesName, Owned: []float64{}}
		have := map[int]bool{}
		maxPos := 0
		st.Owned = r.Positions
		for _, f := range r.Positions {
			if f == math.Trunc(f) && f >= 1 && f <= 10000 {
				have[int(f)] = true
				maxPos = max(maxPos, int(f))
			}
		}
		for i := 1; i <= maxPos && len(st.Missing) < 200; i++ {
			if !have[i] {
				st.Missing = append(st.Missing, i)
			}
		}
		out = append(out, st)
	}
	return out, nil
}

// AddMissingToList adds every missing volume of a series to a list in one action. Volumes
// already on the list are skipped. It returns how many items were added.
func (s *Service) AddMissingToList(ctx context.Context, libraryID uuid.UUID, series string, listID uuid.UUID) (int, error) {
	all, err := s.Series(ctx, libraryID)
	if err != nil {
		return 0, err
	}
	if _, _, err := s.authorize(ctx, listID, true); err != nil {
		return 0, err
	}
	existing, err := s.q.ListListItems(ctx, dbsqlc.ListListItemsParams{ListID: listID, ReadableLibraries: []uuid.UUID{}})
	if err != nil {
		return 0, err
	}
	onList := map[int]bool{}
	for _, it := range existing {
		if it.SeriesName == series && it.SeriesPosition != nil {
			onList[int(*it.SeriesPosition)] = true
		}
	}
	added := 0
	for _, st := range all {
		if st.Name != series {
			continue
		}
		for _, n := range st.Missing {
			if onList[n] {
				continue
			}
			pos := float64(n)
			if _, err := s.q.AddListItem(ctx, dbsqlc.AddListItemParams{ListID: listID, Title: fmt.Sprintf("%s, vol. %d", series, n),
				Authors: []string{}, SeriesName: series, SeriesPosition: &pos}); err != nil {
				return added, err
			}
			added++
		}
		return added, nil
	}
	return 0, ErrNotFound
}
