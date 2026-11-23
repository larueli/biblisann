package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/larueli/biblisann/internal/authz"
	"github.com/larueli/biblisann/internal/catalog"
	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/library"
	"github.com/larueli/biblisann/internal/storage"
)

type editionBody struct {
	ID            uuid.UUID                     `json:"id"`
	WorkID        uuid.UUID                     `json:"work_id"`
	ISBN13        *string                       `json:"isbn13,omitempty"`
	ISBN10        *string                       `json:"isbn10,omitempty"`
	Title         string                        `json:"title"`
	Subtitle      string                        `json:"subtitle,omitempty"`
	Authors       []string                      `json:"authors" nullable:"false"`
	Publisher     string                        `json:"publisher,omitempty"`
	PublishedYear *int32                        `json:"published_year,omitempty"`
	Language      string                        `json:"language,omitempty"`
	Pages         *int32                        `json:"pages,omitempty"`
	Description   string                        `json:"description,omitempty" doc:"Provider text: untrusted."`
	SeriesName    string                        `json:"series_name,omitempty"`
	SeriesPos     *float64                      `json:"series_position,omitempty"`
	CoverURL      string                        `json:"cover_url,omitempty"`
	Provenance    map[string]catalog.Provenance `json:"provenance"`
}

func toEditionBody(e dbsqlc.Edition) editionBody {
	b := editionBody{ID: e.ID, WorkID: e.WorkID, ISBN13: e.Isbn13, ISBN10: e.Isbn10, Title: e.Title, Subtitle: e.Subtitle,
		Authors: nonNil(e.Authors), Publisher: e.Publisher, PublishedYear: e.PublishedYear, Language: e.Language, Pages: e.Pages,
		Description: e.Description, SeriesName: e.SeriesName, SeriesPos: e.SeriesPosition, Provenance: map[string]catalog.Provenance{}}
	if e.CoverID != nil {
		b.CoverURL = "/v1/covers/" + e.CoverID.String()
	}
	_ = json.Unmarshal(e.Provenance, &b.Provenance)
	return b
}

type ownedBody struct {
	InstanceID  uuid.UUID `json:"instance_id"`
	LibraryID   uuid.UUID `json:"library_id"`
	LibraryName string    `json:"library_name"`
	EditionID   uuid.UUID `json:"edition_id"`
	Title       string    `json:"title"`
	SameEdition bool      `json:"same_edition" doc:"false means another edition of the same work."`
}

func toOwned(m []library.OwnedMatch) []ownedBody {
	out := make([]ownedBody, len(m)) // never nil: the JSON array is never null
	for i, o := range m {
		out[i] = ownedBody{o.InstanceID, o.LibraryID, o.LibraryName, o.EditionID, o.Title, o.SameEdition}
	}
	return out
}

type draftBody struct {
	ISBN13      string                        `json:"isbn13"`
	ISBN10      string                        `json:"isbn10,omitempty"`
	Title       string                        `json:"title"`
	Subtitle    string                        `json:"subtitle,omitempty"`
	Authors     []string                      `json:"authors" nullable:"false"`
	Publisher   string                        `json:"publisher,omitempty"`
	Year        int                           `json:"published_year,omitempty"`
	Language    string                        `json:"language,omitempty"`
	Pages       int                           `json:"pages,omitempty"`
	Description string                        `json:"description,omitempty" doc:"Provider text: untrusted."`
	Series      string                        `json:"series_name,omitempty"`
	Provenance  map[string]catalog.Provenance `json:"provenance"`
}

func toDraftBody(d *catalog.Draft) *draftBody {
	if d == nil {
		return nil
	}
	return &draftBody{d.ISBN13, d.ISBN10, d.Title, d.Subtitle, nonNil(d.Authors), d.Publisher, d.Year, d.Language, d.Pages,
		d.Description, d.Series, d.Provenance}
}

type lookupBody struct {
	ISBN13  string            `json:"isbn13"`
	Source  string            `json:"source" enum:"catalog,providers,none" doc:"catalog: already known locally, no external request was made."`
	Edition *editionBody      `json:"edition,omitempty"`
	Draft   *draftBody        `json:"draft,omitempty"`
	Errors  map[string]string `json:"provider_errors,omitempty"`
	Owned   []ownedBody       `json:"owned" doc:"Copies I already own: same edition or another edition of the same work."`
}

// settingsFrom parses stored lookup settings (library or user level).
func settingsFrom(raw []byte) *catalog.LookupSettings {
	var ls catalog.LookupSettings
	if json.Unmarshal(raw, &ls) != nil {
		return nil
	}
	return &ls
}

func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (s *Server) registerCatalog(a huma.API) {
	Register(a, huma.Operation{OperationID: "lookup-isbn", Method: http.MethodGet, Path: "/v1/isbn/{isbn}",
		Summary: "Look an ISBN up (local catalog first, then metadata providers) and tell whether I own it",
		Tags:    []string{"catalog"}, Errors: []int{422}}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			ISBN      string    `path:"isbn" maxLength:"32"`
			Providers string    `query:"providers" maxLength:"200" doc:"Comma-separated provider names for this request only."`
			Languages string    `query:"languages" maxLength:"100" doc:"Comma-separated preferred languages for this request only."`
			LibraryID uuid.UUID `query:"library_id" doc:"Apply this library's lookup settings."`
		}) (*struct{ Body lookupBody }, error) {
			req := &catalog.LookupSettings{}
			if p := splitList(in.Providers); p != nil {
				req.Providers = p
			}
			if l := splitList(in.Languages); l != nil {
				req.Languages = l
			}
			var libSettings *catalog.LookupSettings
			if in.LibraryID != uuid.Nil {
				l, err := s.Library.GetLibrary(ctx, in.LibraryID)
				if err != nil {
					return nil, mapError(s.Log, err)
				}
				libSettings = settingsFrom(l.Settings)
			}
			res, err := s.Catalog.Lookup(ctx, in.ISBN, req, libSettings)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			owned, err := s.Library.Owned(ctx, res.ISBN13)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			out := lookupBody{ISBN13: res.ISBN13, Source: "none", Errors: res.Errors, Owned: toOwned(owned)}
			switch {
			case res.Existing != nil:
				e := toEditionBody(*res.Existing)
				out.Source, out.Edition = "catalog", &e
			case res.Draft != nil:
				out.Source, out.Draft = "providers", toDraftBody(res.Draft)
			}
			return &struct{ Body lookupBody }{out}, nil
		})

	Register(a, huma.Operation{OperationID: "list-providers", Method: http.MethodGet, Path: "/v1/providers",
		Summary: "List the metadata providers this server offers", Tags: []string{"catalog"}}, PermAuthenticated,
		func(context.Context, *struct{}) (*struct{ Body []string }, error) {
			return &struct{ Body []string }{s.Catalog.ProviderNames()}, nil
		})

	Register(a, huma.Operation{OperationID: "create-edition", Method: http.MethodPost, Path: "/v1/editions",
		Summary: "Create an edition by hand (also for books without ISBN)", Tags: []string{"catalog"},
		DefaultStatus: http.StatusCreated, Errors: []int{422}}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			Body struct {
				ISBN        string   `json:"isbn,omitempty" maxLength:"32"`
				Title       string   `json:"title" minLength:"1" maxLength:"1000"`
				Subtitle    string   `json:"subtitle,omitempty" maxLength:"1000"`
				Authors     []string `json:"authors,omitempty" maxItems:"50"`
				Publisher   string   `json:"publisher,omitempty" maxLength:"300"`
				Year        int      `json:"published_year,omitempty" minimum:"0" maximum:"3000"`
				Language    string   `json:"language,omitempty" maxLength:"10"`
				Pages       int      `json:"pages,omitempty" minimum:"0" maximum:"100000"`
				Description string   `json:"description,omitempty" maxLength:"20000"`
				Series      string   `json:"series_name,omitempty" maxLength:"300"`
			}
		}) (*struct{ Body editionBody }, error) {
			p, err := authz.From(ctx)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			if !p.HasScope("write") {
				return nil, huma.Error403Forbidden("forbidden")
			}
			b := in.Body
			e, _, err := s.Catalog.Create(ctx, p.UserID, catalog.EditionInput{ISBN: b.ISBN, Title: b.Title, Subtitle: b.Subtitle,
				Authors: b.Authors, Publisher: b.Publisher, Year: b.Year, Language: b.Language, Pages: b.Pages,
				Description: b.Description, Series: b.Series}, "", map[string]catalog.Provenance{"title": {Provider: "manual", FetchedAt: time.Now()}})
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			return &struct{ Body editionBody }{toEditionBody(e)}, nil
		})

	Register(a, huma.Operation{OperationID: "get-edition", Method: http.MethodGet, Path: "/v1/editions/{id}",
		Summary: "Get an edition", Tags: []string{"catalog"}}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			ID uuid.UUID `path:"id"`
		}) (*struct{ Body editionBody }, error) {
			e, err := s.Catalog.Get(ctx, in.ID)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			return &struct{ Body editionBody }{toEditionBody(e)}, nil
		})

	Register(a, huma.Operation{OperationID: "owned", Method: http.MethodGet, Path: "/v1/owned/{isbn}",
		Summary: "Do I own this? (same edition or another edition of the work, in any library I can read)",
		Tags:    []string{"catalog"}, Errors: []int{422}}, PermAuthenticated,
		func(ctx context.Context, in *struct {
			ISBN string `path:"isbn" maxLength:"32"`
		}) (*struct{ Body []ownedBody }, error) {
			m, err := s.Library.Owned(ctx, in.ISBN)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			return &struct{ Body []ownedBody }{toOwned(m)}, nil
		})

	type scanResult struct {
		Status   string        `json:"status" enum:"added,already_owned,not_found" doc:"not_found: no provider knows this ISBN; add it by hand."`
		Instance *instanceBody `json:"instance,omitempty"`
		Edition  *editionBody  `json:"edition,omitempty"`
		Owned    []ownedBody   `json:"owned,omitempty"`
	}
	Register(a, huma.Operation{OperationID: "scan", Method: http.MethodPost, Path: "/v1/libraries/{id}/scan",
		Summary: "Add a scanned ISBN to a library (safe to replay with the same Idempotency-Key)",
		Tags:    []string{"catalog"}, Errors: []int{409, 422}}, PermLibraryEdit,
		func(ctx context.Context, in *struct {
			ID             uuid.UUID `path:"id"`
			IdempotencyKey string    `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
			Body           struct {
				ISBN           string     `json:"isbn" maxLength:"32"`
				AllowDuplicate bool       `json:"allow_duplicate,omitempty" doc:"Add the copy even if I already own this work."`
				PlaceID        *uuid.UUID `json:"place_id,omitempty"`
			}
		}) (*struct{ Body scanResult }, error) {
			out := scanResult{}
			// A replay returns the first answer, even though the first call changed what I own.
			if prev, err := s.Library.ReplayedInstance(ctx, in.IdempotencyKey); err != nil {
				return nil, mapError(s.Log, err)
			} else if prev != nil {
				b := toInstanceBody(*prev)
				out.Status, out.Instance = "added", &b
				return &struct{ Body scanResult }{out}, nil
			}
			req := &catalog.LookupSettings{}
			lib, err := s.Library.GetLibrary(ctx, in.ID)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			res, err := s.Catalog.Lookup(ctx, in.Body.ISBN, req, settingsFrom(lib.Settings))
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			owned, err := s.Library.Owned(ctx, res.ISBN13)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			if len(owned) > 0 && !in.Body.AllowDuplicate {
				out.Status, out.Owned = "already_owned", toOwned(owned)
				return &struct{ Body scanResult }{out}, nil
			}
			var ed dbsqlc.Edition
			switch {
			case res.Existing != nil:
				ed = *res.Existing
			case res.Draft != nil:
				p, _ := authz.From(ctx)
				d := res.Draft
				ed, _, err = s.Catalog.Create(ctx, p.UserID, catalog.EditionInput{ISBN: d.ISBN13, Title: d.Title, Subtitle: d.Subtitle,
					Authors: d.Authors, Publisher: d.Publisher, Year: d.Year, Language: d.Language, Pages: d.Pages,
					Description: d.Description, Series: d.Series, SeriesPos: d.SeriesPos}, d.CoverURL, d.Provenance)
				if err != nil {
					return nil, mapError(s.Log, err)
				}
			default:
				out.Status = "not_found"
				return &struct{ Body scanResult }{out}, nil
			}
			inst, err := s.Library.AddInstance(ctx, in.ID, library.InstanceInput{EditionID: ed.ID, PlaceID: in.Body.PlaceID}, in.IdempotencyKey)
			if err != nil {
				return nil, mapError(s.Log, err)
			}
			ib, eb := toInstanceBody(inst), toEditionBody(ed)
			out.Status, out.Instance, out.Edition = "added", &ib, &eb
			return &struct{ Body scanResult }{out}, nil
		})
}

// serveCover streams a stored cover. It is always image/jpeg (re-encoded at upload), never the
// original content type, and may be cached by the browser: covers are immutable by ID.
func (s *Server) serveCover(w http.ResponseWriter, r *http.Request) {
	if s.Catalog == nil {
		writeProblem(w, http.StatusNotFound, "not found")
		return
	}
	if _, err := authz.From(r.Context()); err != nil {
		writeProblem(w, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeProblem(w, http.StatusNotFound, "not found")
		return
	}
	rc, err := s.Catalog.CoverReader(r.Context(), id, r.URL.Query().Get("thumb") == "1")
	if errors.Is(err, storage.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.Log.Error("cover", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer func() { _ = rc.Close() }()
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	_, _ = io.Copy(w, rc)
}
