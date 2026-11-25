package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/imaging"
	"github.com/larueli/biblisann/internal/providers"
	"github.com/larueli/biblisann/internal/safehttp"
	"github.com/larueli/biblisann/internal/storage"
)

// Cache lifetimes for provider answers. Misses are retried sooner than hits are refreshed.
const (
	cacheHitTTL  = 30 * 24 * time.Hour
	cacheMissTTL = 24 * time.Hour
)

// Fetcher downloads covers. *safehttp.Client satisfies it.
type Fetcher interface {
	Get(ctx context.Context, url string) (*safehttp.Response, error)
}

// Config configures the Service.
type Config struct {
	Providers         []providers.Provider // available providers
	DefaultProviders  []string             // instance default order
	DisabledProviders []string             // disabled by the administrator
	Covers            storage.Store
	Fetcher           Fetcher
	Log               *slog.Logger
}

// Service is the catalog service.
type Service struct {
	pool interface {
		Begin(context.Context) (pgx.Tx, error)
	}
	q        *dbsqlc.Queries
	byName   map[string]providers.Provider
	known    []string
	defaults []string
	disabled []string
	covers   storage.Store
	fetcher  Fetcher
	log      *slog.Logger
	now      func() time.Time
	access   Access
}

// WithTx returns a transaction-bound catalog service. Internal transactions use savepoints.
func (s *Service) WithTx(tx pgx.Tx) *Service {
	copy := *s
	copy.pool, copy.q = tx, s.q.WithTx(tx)
	return &copy
}

// NewService creates a Service.
func NewService(pool *pgxpool.Pool, cfg Config) *Service {
	s := &Service{pool: pool, q: dbsqlc.New(pool), byName: map[string]providers.Provider{},
		defaults: cfg.DefaultProviders, disabled: cfg.DisabledProviders, covers: cfg.Covers,
		fetcher: cfg.Fetcher, log: cfg.Log, now: time.Now}
	if s.log == nil {
		s.log = slog.Default()
	}
	for _, p := range cfg.Providers {
		s.byName[p.Name()] = p
		s.known = append(s.known, p.Name())
	}
	if len(s.defaults) == 0 {
		s.defaults = s.known
	}
	return s
}

// ProviderNames lists the available providers.
func (s *Service) ProviderNames() []string { return append([]string(nil), s.known...) }

// LookupResult is the outcome of an ISBN lookup.
type LookupResult struct {
	ISBN13   string
	ISBN10   string
	Existing *dbsqlc.Edition   // already in the local catalog: no external request was made
	Draft    *Draft            // merged provider metadata, not stored
	Errors   map[string]string // provider name -> failure kind, for providers that failed
}

// Lookup normalizes the ISBN, answers from the local catalog when possible (so nothing about the
// book leaves the server), and otherwise queries the providers selected by the settings layers.
func (s *Service) Lookup(ctx context.Context, rawISBN string, layers ...*LookupSettings) (*LookupResult, error) {
	isbn13, isbn10, err := NormalizeISBN(rawISBN)
	if err != nil {
		return nil, err
	}
	res := &LookupResult{ISBN13: isbn13, ISBN10: isbn10}
	if e, err := s.q.GetEditionByISBN13(ctx, &isbn13); err == nil {
		res.Existing = &e
		return res, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("catalog: lookup edition: %w", err)
	}

	res.Draft, res.Errors = s.providerDraft(ctx, isbn13, layers...)
	if res.Draft != nil {
		res.Draft.ISBN13, res.Draft.ISBN10 = isbn13, isbn10
	}
	return res, nil
}

// providerDraft asks the configured providers about an ISBN and merges their answers. The error
// map lists providers that failed.
func (s *Service) providerDraft(ctx context.Context, isbn13 string, layers ...*LookupSettings) (*Draft, map[string]string) {
	var errs map[string]string
	set := ResolveSettings(s.known, s.disabled, layers...)
	names := set.Providers
	if names == nil {
		names = ResolveSettings(s.known, s.disabled, &LookupSettings{Providers: s.defaults}).Providers
	}
	var results []Result
	for _, name := range names {
		r, ferr := s.fromProvider(ctx, s.byName[name], isbn13)
		if ferr != nil {
			if errs == nil {
				errs = map[string]string{}
			}
			errs[name] = "unavailable"
			s.log.WarnContext(ctx, "provider lookup failed", "provider", name, "error", ferr)
			continue
		}
		results = append(results, r)
	}
	return Merge(results, set.Languages), errs
}

type cachedRecord struct {
	Record *providers.Record `json:"record"`
}

func (s *Service) fromProvider(ctx context.Context, p providers.Provider, isbn13 string) (Result, error) {
	c, err := s.q.GetProviderCache(ctx, dbsqlc.GetProviderCacheParams{Provider: p.Name(), Key: isbn13})
	if err == nil {
		ttl := cacheHitTTL
		if !c.Found {
			ttl = cacheMissTTL
		}
		if s.now().Sub(c.FetchedAt) < ttl {
			var cr cachedRecord
			if json.Unmarshal(c.Payload, &cr) == nil {
				return Result{Provider: p.Name(), Record: cr.Record, FetchedAt: c.FetchedAt}, nil
			}
		}
	}
	rec, err := p.Lookup(ctx, isbn13)
	if err != nil {
		return Result{}, err
	}
	payload, _ := json.Marshal(cachedRecord{Record: rec})
	if err := s.q.PutProviderCache(ctx, dbsqlc.PutProviderCacheParams{
		Provider: p.Name(), Key: isbn13, Found: rec != nil, Payload: payload}); err != nil {
		s.log.WarnContext(ctx, "cache write failed", "error", err)
	}
	return Result{Provider: p.Name(), Record: rec, FetchedAt: s.now()}, nil
}

// ErrInvalidEdition is returned for a draft that cannot be stored.
var ErrInvalidEdition = errors.New("invalid edition")

// EditionInput is what a client may submit to create an edition (manual entry or a confirmed
// draft). Client-supplied provenance is ignored: it is always set by the server.
type EditionInput struct {
	ISBN        string
	Title       string
	Subtitle    string
	Authors     []string
	Publisher   string
	Year        int
	Language    string
	Pages       int
	Description string
	Series      string
	SeriesPos   *float64
}

// tooLong enforces the field limits of the API for every path into the shared catalog (imports and
// providers included), so one oversized row cannot be served to every user.
func tooLong(in EditionInput) bool {
	if len(in.Description) > 20000 || len(in.Subtitle) > 1000 || len(in.Publisher) > 300 || len(in.Language) > 35 || len(in.Series) > 300 {
		return true
	}
	for _, a := range in.Authors {
		if len(a) > 300 {
			return true
		}
	}
	return false
}

// Create stores an edition (and its work), reusing an existing edition with the same ISBN. The
// returned bool says whether a new edition was created.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, in EditionInput, coverURL string, prov map[string]Provenance) (dbsqlc.Edition, bool, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" || len(title) > 1000 || len(in.Authors) > 50 || tooLong(in) {
		return dbsqlc.Edition{}, false, ErrInvalidEdition
	}
	var isbn13, isbn10 *string
	if strings.TrimSpace(in.ISBN) != "" {
		a, b, err := NormalizeISBN(in.ISBN)
		if err != nil {
			return dbsqlc.Edition{}, false, err
		}
		isbn13 = &a
		if b != "" {
			isbn10 = &b
		}
		if e, err := s.q.GetEditionByISBN13(ctx, isbn13); err == nil {
			return e, false, nil
		}
	}
	authors := cleanList(in.Authors)

	coverID := s.fetchCover(ctx, coverURL)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return dbsqlc.Edition{}, false, fmt.Errorf("catalog: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	work, err := q.FindWorkByTitleAuthors(ctx, dbsqlc.FindWorkByTitleAuthorsParams{Column1: title, Column2: authors})
	if errors.Is(err, pgx.ErrNoRows) {
		work, err = q.CreateWork(ctx, dbsqlc.CreateWorkParams{Title: title, Authors: authors})
	}
	if err != nil {
		return dbsqlc.Edition{}, false, fmt.Errorf("catalog: work: %w", err)
	}
	if isbn13 == nil { // the same manual entry twice is the same edition
		var y *int32
		if in.Year > 0 {
			v := int32(in.Year)
			y = &v
		}
		if e, err := q.FindIsbnlessEdition(ctx, dbsqlc.FindIsbnlessEditionParams{WorkID: work.ID,
			Publisher: strings.TrimSpace(in.Publisher), Language: strings.TrimSpace(in.Language), Year: y}); err == nil {
			return e, false, nil
		}
	}
	provJSON, _ := json.Marshal(prov)
	if prov == nil {
		provJSON = []byte("{}")
	}
	var year, pages *int32
	if in.Year > 0 {
		y := int32(in.Year)
		year = &y
	}
	if in.Pages > 0 {
		p := int32(in.Pages)
		pages = &p
	}
	e, err := q.CreateEdition(ctx, dbsqlc.CreateEditionParams{
		WorkID: work.ID, Isbn13: isbn13, Isbn10: isbn10, Title: title, Subtitle: strings.TrimSpace(in.Subtitle),
		Authors: authors, Publisher: strings.TrimSpace(in.Publisher), PublishedYear: year,
		Language: strings.TrimSpace(in.Language), Pages: pages, Description: in.Description,
		SeriesName: strings.TrimSpace(in.Series), SeriesPosition: in.SeriesPos, CoverID: coverID,
		Provenance: provJSON, CreatedBy: &userID,
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && isbn13 != nil {
		_ = tx.Rollback(ctx)
		existing, gerr := s.q.GetEditionByISBN13(ctx, isbn13) // lost a race: reuse the winner
		return existing, false, gerr
	}
	if err != nil {
		return dbsqlc.Edition{}, false, fmt.Errorf("catalog: create edition: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return dbsqlc.Edition{}, false, fmt.Errorf("catalog: commit: %w", err)
	}
	return e, true, nil
}

// fetchCover downloads, re-encodes and stores a cover. Failures are logged, never fatal: a book
// without a cover is still a book.
func (s *Service) fetchCover(ctx context.Context, url string) *uuid.UUID {
	if url == "" || s.fetcher == nil || s.covers == nil {
		return nil
	}
	resp, err := s.fetcher.Get(ctx, url)
	if err != nil {
		s.log.WarnContext(ctx, "cover fetch failed", "error", err)
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.Status != 200 {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, imaging.MaxInputBytes+1))
	if err != nil {
		return nil
	}
	return s.StoreCover(ctx, data)
}

// StoreCover re-encodes untrusted image bytes and stores them content-addressed.
func (s *Service) StoreCover(ctx context.Context, data []byte) *uuid.UUID {
	img, err := imaging.Process(data)
	if err != nil {
		s.log.WarnContext(ctx, "cover rejected", "error", err)
		return nil
	}
	sum := sha256.Sum256(img.Full)
	key := hex.EncodeToString(sum[:])
	if err := s.covers.Put(ctx, key, img.Full); err != nil {
		s.log.ErrorContext(ctx, "cover store failed", "error", err)
		return nil
	}
	if err := s.covers.Put(ctx, key+".thumb", img.Thumb); err != nil {
		s.log.ErrorContext(ctx, "cover thumb store failed", "error", err)
		return nil
	}
	c, err := s.q.InsertCover(ctx, dbsqlc.InsertCoverParams{Sha256: sum[:], Width: int32(img.Width), Height: int32(img.Height)})
	if err != nil {
		s.log.ErrorContext(ctx, "cover row failed", "error", err)
		return nil
	}
	return &c.ID
}

// CoverReader opens a stored cover (thumb selects the thumbnail variant).
func (s *Service) CoverReader(ctx context.Context, id uuid.UUID, thumb bool) (io.ReadCloser, error) {
	c, err := s.q.GetCover(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("catalog: cover: %w", err)
	}
	key := hex.EncodeToString(c.Sha256)
	if thumb {
		key += ".thumb"
	}
	return s.covers.Open(ctx, key)
}

// Get returns an edition by ID.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (dbsqlc.Edition, error) {
	e, err := s.q.GetEdition(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

// ErrNotFound is returned for unknown editions.
var ErrNotFound = errors.New("edition not found")

func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, a := range in {
		if a = strings.Join(strings.Fields(a), " "); a != "" && len(a) <= 300 {
			out = append(out, a)
		}
	}
	return out
}

// AttachCover sets the edition's cover when it has none (used by imports).
func (s *Service) AttachCover(ctx context.Context, editionID, coverID uuid.UUID) error {
	return s.q.SetEditionCoverIfMissing(ctx, dbsqlc.SetEditionCoverIfMissingParams{ID: editionID, CoverID: &coverID})
}

// CoverHash returns the SHA-256 hex of a stored cover, which names its file in archives.
func (s *Service) CoverHash(ctx context.Context, id uuid.UUID) (string, error) {
	b, err := s.q.GetCoverSHA(ctx, id)
	return hex.EncodeToString(b), err
}
