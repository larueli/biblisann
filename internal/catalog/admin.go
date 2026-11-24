package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
)

// Errors of the administrator operations.
var (
	ErrSyncExcluded = errors.New("this edition is excluded from provider refreshes")
	ErrNoISBN       = errors.New("this edition has no ISBN to look up")
	ErrInUse        = errors.New("a library still holds copies of this edition")
	ErrNothingFound = errors.New("no provider knows this ISBN")
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

// Update applies a manual edit to a shared edition. The caller must be an administrator.
func (s *Service) Update(ctx context.Context, id uuid.UUID, p EditionPatch) (dbsqlc.Edition, error) {
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
	e, err := s.q.SetEditionSyncExcluded(ctx, dbsqlc.SetEditionSyncExcludedParams{ID: id, SyncExcluded: excluded})
	if errors.Is(err, pgx.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

// Refresh asks the providers again and replaces the fields that are not locked by a manual edit.
// Fields no provider knows are kept. An edition excluded from refreshes is never touched.
func (s *Service) Refresh(ctx context.Context, id uuid.UUID, layers ...*LookupSettings) (dbsqlc.Edition, error) {
	e, err := s.Get(ctx, id)
	if err != nil {
		return e, err
	}
	if e.SyncExcluded {
		return e, ErrSyncExcluded
	}
	if e.Isbn13 == nil {
		return e, ErrNoISBN
	}
	if err := s.q.DeleteProviderCacheForKey(ctx, *e.Isbn13); err != nil { // an administrator wants fresh answers
		return e, err
	}
	draft, _ := s.providerDraft(ctx, *e.Isbn13, layers...)
	if draft == nil {
		return e, ErrNothingFound
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
		return e, ErrInvalidEdition
	}
	return s.store(ctx, e, prov, locked)
}

// Delete removes an edition no library holds any more.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
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
