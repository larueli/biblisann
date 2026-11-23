package library

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/catalog"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
)

// Page limits.
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

type cursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"i"`
}

func encodeCursor(c cursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (cursor, error) {
	if s == "" {
		return cursor{T: time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC), ID: uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")}, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	var c cursor
	if err != nil || json.Unmarshal(b, &c) != nil {
		return c, ErrInvalid
	}
	return c, nil
}

func clampLimit(n int) int {
	switch {
	case n <= 0:
		return DefaultPageSize
	case n > MaxPageSize:
		return MaxPageSize
	}
	return n
}

// InstancePage is one page of a library listing.
type InstancePage struct {
	Items      []dbsqlc.ListInstancesRow
	NextCursor string
}

// ListInstances lists copies, newest first, with cursor pagination.
func (s *Service) ListInstances(ctx context.Context, libraryID uuid.UUID, cur string, limit int) (InstancePage, error) {
	if _, err := s.Require(ctx, nil, libraryID, authz.RoleRead); err != nil {
		return InstancePage{}, err
	}
	c, err := decodeCursor(cur)
	if err != nil {
		return InstancePage{}, err
	}
	limit = clampLimit(limit)
	rows, err := s.q.ListInstances(ctx, dbsqlc.ListInstancesParams{LibraryID: libraryID, Column2: c.T, Column3: c.ID, Limit: int32(limit + 1)})
	if err != nil {
		return InstancePage{}, fmt.Errorf("library: list: %w", err)
	}
	page := InstancePage{Items: rows}
	if len(rows) > limit {
		page.Items = rows[:limit]
		last := rows[limit-1]
		page.NextCursor = encodeCursor(cursor{T: last.CreatedAt, ID: last.ID})
	}
	return page, nil
}

// SearchInstances searches the copies of one library by title, author, publisher or ISBN. The
// text is accent- and case-insensitive; typos are tolerated through trigram matching.
func (s *Service) SearchInstances(ctx context.Context, libraryID uuid.UUID, query string, limit int) ([]dbsqlc.SearchInstancesRow, error) {
	if _, err := s.Require(ctx, nil, libraryID, authz.RoleRead); err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 200 {
		return nil, ErrInvalid
	}
	return s.q.SearchInstances(ctx, dbsqlc.SearchInstancesParams{LibraryID: libraryID, Column2: query, Limit: int32(clampLimit(limit))})
}

// OwnedMatch is one copy answering "do I own this?".
type OwnedMatch struct {
	InstanceID  uuid.UUID
	LibraryID   uuid.UUID
	LibraryName string
	EditionID   uuid.UUID
	Title       string
	SameEdition bool
}

// Owned answers "do I own this?" across every library the caller can read, at Work level: the
// same edition and other editions of the same work both match.
func (s *Service) Owned(ctx context.Context, rawISBN string) ([]OwnedMatch, error) {
	p, err := authz.From(ctx)
	if err != nil {
		return nil, err
	}
	if !p.HasScope("read") && !p.HasScope("write") {
		return nil, ErrForbidden
	}
	isbn13, _, err := catalog.NormalizeISBN(rawISBN)
	if err != nil {
		return nil, err
	}
	workID := uuid.Nil
	if e, err := s.q.GetEditionByISBN13(ctx, &isbn13); err == nil {
		workID = e.WorkID
	}
	rows, err := s.q.OwnedByWorkOrISBN(ctx, dbsqlc.OwnedByWorkOrISBNParams{UserID: p.UserID, Column2: isbn13, Column3: workID})
	if err != nil {
		return nil, fmt.Errorf("library: owned: %w", err)
	}
	out := make([]OwnedMatch, len(rows))
	for i, r := range rows {
		out[i] = OwnedMatch{InstanceID: r.InstanceID, LibraryID: r.LibraryID, LibraryName: r.LibraryName,
			EditionID: r.EditionID, Title: r.Title, SameEdition: r.SameEdition}
	}
	return out, nil
}

// IndexEntry is one row of the compact offline index.
type IndexEntry struct {
	ID        uuid.UUID  `json:"id"`
	EditionID uuid.UUID  `json:"edition_id,omitempty"`
	WorkID    uuid.UUID  `json:"work_id,omitempty"`
	Title     string     `json:"title,omitempty"`
	Authors   []string   `json:"authors,omitempty" nullable:"false"`
	ISBN13    *string    `json:"isbn13,omitempty"`
	PlaceID   *uuid.UUID `json:"place_id,omitempty"`
	Deleted   bool       `json:"deleted"`
}

// Index is a page of changes since a cursor. Deleted entries are tombstones.
type Index struct {
	Cursor  int64
	Entries []IndexEntry
	HasMore bool
	// Reset tells the client its cursor is older than the retained history (deletions were
	// forgotten): it must drop its local copy of the library and download everything again.
	Reset bool
}

// SyncIndex returns the changes of a library after the cursor (0 means everything). The client
// stores Cursor and passes it back; entries with Deleted=true must be removed locally.
func (s *Service) SyncIndex(ctx context.Context, libraryID uuid.UUID, since int64, limit int) (Index, error) {
	if _, err := s.Require(ctx, nil, libraryID, authz.RoleRead); err != nil {
		return Index{}, err
	}
	if since < 0 {
		return Index{}, ErrInvalid
	}
	limit = clampLimit(limit)
	if limit < 100 {
		limit = 100
	}
	reset := false
	if since > 0 {
		if horizon, err := s.q.GetPrunedThrough(ctx, libraryID); err == nil && since < horizon {
			reset, since = true, 0
		}
	}
	changes, err := s.q.ListChanges(ctx, dbsqlc.ListChangesParams{LibraryID: libraryID, Seq: since, Limit: int32(limit + 1)})
	if err != nil {
		return Index{}, fmt.Errorf("library: changes: %w", err)
	}
	idx := Index{Cursor: since, Reset: reset}
	if len(changes) > limit {
		changes = changes[:limit]
		idx.HasMore = true
	}
	if len(changes) == 0 {
		return idx, nil
	}
	idx.Cursor = changes[len(changes)-1].Seq
	seen := map[uuid.UUID]bool{}
	var ids []uuid.UUID
	for _, c := range changes {
		if c.Entity == "instance" && !seen[c.EntityID] {
			seen[c.EntityID] = true
			ids = append(ids, c.EntityID)
		}
	}
	rows, err := s.q.SyncInstanceRows(ctx, ids)
	if err != nil {
		return Index{}, fmt.Errorf("library: sync rows: %w", err)
	}
	byID := map[uuid.UUID]dbsqlc.SyncInstanceRowsRow{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	for _, id := range ids {
		r, ok := byID[id]
		if !ok || r.Deleted {
			idx.Entries = append(idx.Entries, IndexEntry{ID: id, Deleted: true})
			continue
		}
		idx.Entries = append(idx.Entries, IndexEntry{ID: id, EditionID: r.EditionID, WorkID: r.WorkID,
			Title: r.Title, Authors: r.Authors, ISBN13: r.Isbn13, PlaceID: r.PlaceID})
	}
	return idx, nil
}

// InventoryResult compares what a place should contain with what was scanned in it.
type InventoryResult struct {
	Found      []string // titles of expected copies that were scanned
	Missing    []string // titles expected in the place but not scanned
	Unexpected []string // scanned ISBNs that belong to no copy expected here
	Unscanable []string // expected copies without an ISBN: they cannot be checked by scanning
}

// Inventory checks a physical place against a list of scanned ISBNs (inventory mode).
func (s *Service) Inventory(ctx context.Context, placeID uuid.UUID, scanned []string) (InventoryResult, error) {
	var res InventoryResult
	pl, err := s.q.GetPlace(ctx, placeID)
	if err != nil {
		return res, ErrNotFound
	}
	if _, err := s.Require(ctx, nil, pl.LibraryID, authz.RoleRead); err != nil {
		return res, err
	}
	if len(scanned) > 5000 {
		return res, ErrInvalid
	}
	seen := map[string]bool{}
	for _, raw := range scanned {
		isbn, _, err := catalog.NormalizeISBN(raw)
		if err != nil {
			return res, ErrInvalid
		}
		seen[isbn] = true
	}
	rows, err := s.q.PlaceSubtreeInstances(ctx, placeID)
	if err != nil {
		return res, fmt.Errorf("library: inventory: %w", err)
	}
	expected := map[string]bool{}
	for _, r := range rows {
		switch {
		case r.Isbn13 == nil:
			res.Unscanable = append(res.Unscanable, r.Title)
		case seen[*r.Isbn13]:
			res.Found = append(res.Found, r.Title)
			expected[*r.Isbn13] = true
		default:
			res.Missing = append(res.Missing, r.Title)
		}
	}
	for isbn := range seen {
		if !expected[isbn] {
			res.Unexpected = append(res.Unexpected, isbn)
		}
	}
	return res, nil
}
