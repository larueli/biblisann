package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/larueli/biblisann/internal/authz"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
)

// Errors of the administrator operations.
var (
	ErrSyncExcluded         = errors.New("this edition is excluded from provider refreshes")
	ErrProvidersUnavailable = errors.New("no provider answered, try again later")
	ErrInUse                = errors.New("a library still holds copies of this edition")
)

// Results of a provider refresh.
const (
	StatusUpdated  = "updated"
	StatusNotFound = "not_found" // no provider knows the ISBN
	StatusNoISBN   = "no_isbn"   // nothing to look up
)

// ManualProvider is the provenance of a field an administrator edited by hand.
const ManualProvider = "manual"

// EditionPatch holds the fields an administrator changes; nil means unchanged. Every changed field
// is locked: a provider refresh never overwrites it.
type EditionPatch struct {
	Title       *string
	Subtitle    *string
	Authors     *[]string
	Publisher   *string
	Year        *int
	Language    *string
	Pages       *int
	Description *string
	Series      *string
	SeriesPos   *float64
}

// adminOnly denies by default: only an administrator's browser session may change the shared
// catalog. Anything else is told the edition does not exist, like every administration route.
func adminOnly(ctx context.Context) error {
	p, err := authz.From(ctx)
	if err != nil {
		return err
	}
	if !p.IsAdmin || p.Via != "session" {
		return authz.ErrNotFound
	}
	return nil
}

// Update applies a manual edit to a shared edition. The caller must be an administrator.
func (s *Service) Update(ctx context.Context, id uuid.UUID, p EditionPatch) (dbsqlc.Edition, error) {
	if err := adminOnly(ctx); err != nil {
		return dbsqlc.Edition{}, err
	}
	e, err := s.Get(ctx, id)
	if err != nil {
		return e, err
	}
	prov := map[string]Provenance{}
	_ = json.Unmarshal(e.Provenance, &prov)
	locked := slices.Clone(e.LockedFields)
	now := s.now()
	mark := func(field string) {
		prov[field] = Provenance{Provider: ManualProvider, FetchedAt: now}
		if !slices.Contains(locked, field) {
			locked = append(locked, field)
		}
	}
	if p.Title != nil {
		e.Title = strings.TrimSpace(*p.Title)
		mark("title")
	}
	if p.Subtitle != nil {
		e.Subtitle = strings.TrimSpace(*p.Subtitle)
		mark("subtitle")
	}
	if p.Authors != nil {
		e.Authors = cleanList(*p.Authors)
		mark("authors")
	}
	if p.Publisher != nil {
		e.Publisher = strings.TrimSpace(*p.Publisher)
		mark("publisher")
	}
	if p.Year != nil {
		e.PublishedYear = nil
		if *p.Year > 0 {
			v := int32(*p.Year)
			e.PublishedYear = &v
		}
		mark("published_year")
	}
	if p.Language != nil {
		e.Language = strings.TrimSpace(*p.Language)
		mark("language")
	}
	if p.Pages != nil {
		e.Pages = nil
		if *p.Pages > 0 {
			v := int32(*p.Pages)
			e.Pages = &v
		}
		mark("pages")
	}
	if p.Description != nil {
		e.Description = *p.Description
		mark("description")
	}
	if p.Series != nil {
		e.SeriesName = strings.TrimSpace(*p.Series)
		e.SeriesPosition = p.SeriesPos
		mark("series_name")
	}
	if e.Title == "" || len(e.Title) > 1000 || len(e.Authors) > 50 ||
		tooLong(EditionInput{Subtitle: e.Subtitle, Authors: e.Authors, Publisher: e.Publisher, Language: e.Language, Description: e.Description, Series: e.SeriesName}) {
		return dbsqlc.Edition{}, ErrInvalidEdition
	}
	return s.store(ctx, e, prov, locked)
}

func (s *Service) store(ctx context.Context, e dbsqlc.Edition, prov map[string]Provenance, locked []string) (dbsqlc.Edition, error) {
	raw, err := json.Marshal(prov)
	if err != nil {
		return dbsqlc.Edition{}, fmt.Errorf("catalog: provenance: %w", err)
	}
	return s.q.UpdateEditionMetadata(ctx, dbsqlc.UpdateEditionMetadataParams{ID: e.ID, Title: e.Title, Subtitle: e.Subtitle,
		Authors: e.Authors, Publisher: e.Publisher, PublishedYear: e.PublishedYear, Language: e.Language, Pages: e.Pages,
		Description: e.Description, SeriesName: e.SeriesName, SeriesPosition: e.SeriesPosition, CoverID: e.CoverID,
		Provenance: raw, LockedFields: locked})
}

// SetSyncExcluded includes or excludes an edition from provider refreshes.
func (s *Service) SetSyncExcluded(ctx context.Context, id uuid.UUID, excluded bool) (dbsqlc.Edition, error) {
	if err := adminOnly(ctx); err != nil {
		return dbsqlc.Edition{}, err
	}
	e, err := s.q.SetEditionSyncExcluded(ctx, dbsqlc.SetEditionSyncExcludedParams{ID: id, SyncExcluded: excluded})
	if errors.Is(err, pgx.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

// Refresh asks the providers again and replaces the fields that are not locked by a manual edit.
// Fields no provider knows are kept. An edition excluded from refreshes is never touched.
func (s *Service) Refresh(ctx context.Context, id uuid.UUID, layers ...*LookupSettings) (dbsqlc.Edition, string, error) {
	if err := adminOnly(ctx); err != nil {
		return dbsqlc.Edition{}, "", err
	}
	return s.refresh(ctx, id, layers...)
}

// RefreshScope selects what a batch refresh looks at.
type RefreshScope struct {
	LibraryID    *uuid.UUID // nil: the whole instance
	IncludeKnown bool       // also editions providers already described
	IgnoreWait   bool       // do not wait a week between two attempts on the same book
	Limit        int        // at most this many editions per run (default 200)
}

// RefreshReport counts what a batch did.
type RefreshReport struct {
	Checked, Updated, NotFound, NoISBN int
	Stopped                            bool // providers were unavailable: the run ended early
}

// RefreshBatch refreshes the editions of a scope one by one, pausing between lookups so providers
// are not hammered. It is the work of the weekly job and of the administrator's on-demand runs, so
// it carries no principal: callers decide who may start it. Excluded editions are never touched.
func (s *Service) RefreshBatch(ctx context.Context, sc RefreshScope, pause time.Duration) (RefreshReport, error) {
	var rep RefreshReport
	limit := sc.Limit
	if limit <= 0 {
		limit = 200
	}
	ids, err := s.q.ListEditionsToRefresh(ctx, dbsqlc.ListEditionsToRefreshParams{LibraryID: sc.LibraryID, IncludeKnown: sc.IncludeKnown, IgnoreWait: sc.IgnoreWait, MaxRows: int32(limit)})
	if err != nil {
		return rep, err
	}
	for i, id := range ids {
		if i > 0 && pause > 0 {
			select {
			case <-ctx.Done():
				return rep, ctx.Err()
			case <-time.After(pause):
			}
		}
		_, status, err := s.refresh(ctx, id)
		switch {
		case errors.Is(err, ErrSyncExcluded), errors.Is(err, ErrNotFound):
			continue
		case errors.Is(err, ErrProvidersUnavailable):
			rep.Stopped = true // no point asking the next hundred books; the next run retries
			return rep, nil
		case err != nil:
			return rep, err
		}
		rep.Checked++
		switch status {
		case StatusUpdated:
			rep.Updated++
		case StatusNotFound:
			rep.NotFound++
		case StatusNoISBN:
			rep.NoISBN++
		}
	}
	return rep, nil
}

func (s *Service) refresh(ctx context.Context, id uuid.UUID, layers ...*LookupSettings) (dbsqlc.Edition, string, error) {
	e, err := s.Get(ctx, id)
	if err != nil {
		return e, "", err
	}
	if e.SyncExcluded {
		return e, "", ErrSyncExcluded
	}
	// A book providers cannot know (no ISBN, or an ISBN nobody lists) is a normal outcome, not an
	// error: the refresh records it, keeps the data as it is, and the administrator can exclude it.
	if e.Isbn13 == nil {
		return s.recordSync(ctx, e, StatusNoISBN)
	}
	if err := s.q.DeleteProviderCacheForKey(ctx, *e.Isbn13); err != nil { // an administrator wants fresh answers
		return e, "", err
	}
	draft, failed := s.providerDraft(ctx, *e.Isbn13, layers...)
	if draft == nil {
		if len(failed) > 0 {
			return e, "", ErrProvidersUnavailable // we do not know: do not record "not found"
		}
		return s.recordSync(ctx, e, StatusNotFound)
	}
	prov := map[string]Provenance{}
	_ = json.Unmarshal(e.Provenance, &prov)
	locked := slices.Clone(e.LockedFields)
	open := func(field string) bool {
		return !slices.Contains(locked, field) && prov[field].Provider != ManualProvider
	}
	take := func(field string) { prov[field] = draft.Provenance[field] }
	if _, ok := draft.Provenance["title"]; ok && open("title") && draft.Title != "" {
		e.Title = draft.Title
		take("title")
	}
	if _, ok := draft.Provenance["subtitle"]; ok && open("subtitle") {
		e.Subtitle = draft.Subtitle
		take("subtitle")
	}
	if _, ok := draft.Provenance["authors"]; ok && open("authors") {
		e.Authors = cleanList(draft.Authors)
		take("authors")
	}
	if _, ok := draft.Provenance["publisher"]; ok && open("publisher") {
		e.Publisher = draft.Publisher
		take("publisher")
	}
	if _, ok := draft.Provenance["published_year"]; ok && open("published_year") && draft.Year > 0 {
		v := int32(draft.Year)
		e.PublishedYear = &v
		take("published_year")
	}
	if _, ok := draft.Provenance["language"]; ok && open("language") {
		e.Language = draft.Language
		take("language")
	}
	if _, ok := draft.Provenance["pages"]; ok && open("pages") && draft.Pages > 0 {
		v := int32(draft.Pages)
		e.Pages = &v
		take("pages")
	}
	if _, ok := draft.Provenance["description"]; ok && open("description") {
		e.Description = draft.Description
		take("description")
	}
	if _, ok := draft.Provenance["series_name"]; ok && open("series_name") {
		e.SeriesName, e.SeriesPosition = draft.Series, draft.SeriesPos
		take("series_name")
	}
	if _, ok := draft.Provenance["cover"]; ok && open("cover") && draft.CoverURL != "" {
		if id := s.fetchCover(ctx, draft.CoverURL); id != nil {
			e.CoverID = id
			take("cover")
		}
	}
	if e.Title == "" || len(e.Title) > 1000 || len(e.Authors) > 50 ||
		tooLong(EditionInput{Subtitle: e.Subtitle, Authors: e.Authors, Publisher: e.Publisher, Language: e.Language, Description: e.Description, Series: e.SeriesName}) {
		return e, "", ErrInvalidEdition
	}
	stored, err := s.store(ctx, e, prov, locked)
	if err != nil {
		return stored, "", err
	}
	return s.recordSync(ctx, stored, StatusUpdated)
}

func (s *Service) recordSync(ctx context.Context, e dbsqlc.Edition, status string) (dbsqlc.Edition, string, error) {
	out, err := s.q.SetEditionSyncResult(ctx, dbsqlc.SetEditionSyncResultParams{ID: e.ID, SyncStatus: status})
	return out, status, err
}

// Delete removes an edition no library holds any more.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	if err := adminOnly(ctx); err != nil {
		return err
	}
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	n, err := s.q.CountEditionInstances(ctx, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrInUse
	}
	if _, err := s.q.DeleteEdition(ctx, id); err != nil {
		return err
	}
	return nil
}
