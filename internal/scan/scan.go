// Package scan implements "add this ISBN to my library": look it up, warn about duplicates,
// create the edition if needed and add the copy. It is shared by the REST API and MCP.
package scan

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/catalog"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/library"
)

// Status of a scan.
const (
	Added        = "added"
	AlreadyOwned = "already_owned"
	NotFound     = "not_found"
)

// Result is the outcome of a scan.
type Result struct {
	Status   string
	Instance *dbsqlc.Instance
	Edition  *dbsqlc.Edition
	Owned    []library.OwnedMatch
}

// Service orchestrates catalog and library.
type Service struct {
	cat *catalog.Service
	lib *library.Service
}

// New creates the service.
func New(c *catalog.Service, l *library.Service) *Service { return &Service{cat: c, lib: l} }

// Input is a scan request.
type Input struct {
	LibraryID      uuid.UUID
	ISBN           string
	AllowDuplicate bool
	PlaceID        *uuid.UUID
	// IdempotencyKey makes replays return the first result. Required for offline clients.
	IdempotencyKey string
}

func settings(l dbsqlc.Library) *catalog.LookupSettings { return SettingsFrom(l.Settings) }

// Scan runs the flow.
func (s *Service) Scan(ctx context.Context, in Input) (Result, error) {
	var out Result
	// Authorize first: a replayed key or a lookup must not work for someone who may not add books here.
	if _, err := s.lib.RequirePerm(ctx, nil, in.LibraryID, authz.PermWrite); err != nil {
		return out, err
	}
	if in.IdempotencyKey != "" {
		prev, err := s.lib.ReplayedInstance(ctx, in.IdempotencyKey)
		if err != nil {
			return out, err
		}
		if prev != nil {
			out.Status, out.Instance = Added, prev
			return out, nil
		}
	}
	lib, err := s.lib.GetLibrary(ctx, in.LibraryID)
	if err != nil {
		return out, err
	}
	res, err := s.cat.Lookup(ctx, in.ISBN, &catalog.LookupSettings{}, settings(lib))
	if err != nil {
		return out, err
	}
	owned, err := s.lib.Owned(ctx, res.ISBN13)
	if err != nil {
		return out, err
	}
	if len(owned) > 0 && !in.AllowDuplicate {
		out.Status, out.Owned = AlreadyOwned, owned
		return out, nil
	}
	var ed dbsqlc.Edition
	switch {
	case res.Existing != nil:
		ed = *res.Existing
	case res.Draft != nil:
		p, perr := authz.From(ctx)
		if perr != nil {
			return out, perr
		}
		d := res.Draft
		ed, _, err = s.cat.Create(ctx, p.UserID, catalog.EditionInput{ISBN: d.ISBN13, Title: d.Title, Subtitle: d.Subtitle,
			Authors: d.Authors, Publisher: d.Publisher, Year: d.Year, Language: d.Language, Pages: d.Pages,
			Description: d.Description, Series: d.Series, SeriesPos: d.SeriesPos}, d.CoverURL, d.Provenance)
		if err != nil {
			return out, err
		}
	default:
		out.Status = NotFound
		return out, nil
	}
	inst, err := s.lib.AddInstance(ctx, in.LibraryID, library.InstanceInput{EditionID: ed.ID, PlaceID: in.PlaceID}, in.IdempotencyKey)
	if err != nil {
		return out, err
	}
	out.Status, out.Instance, out.Edition = Added, &inst, &ed
	return out, nil
}

// SettingsFrom parses stored lookup settings (library or user level); nil when absent or invalid.
func SettingsFrom(raw []byte) *catalog.LookupSettings {
	var ls catalog.LookupSettings
	if json.Unmarshal(raw, &ls) != nil {
		return nil
	}
	return &ls
}
