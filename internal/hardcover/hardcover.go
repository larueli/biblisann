// Package hardcover fetches community ratings from Hardcover (https://hardcover.app) and stores
// them as external grades, shown separately from the user's own grades.
//
// The GraphQL query below follows Hardcover's public schema (editions filtered by isbn_13, with
// the book's rating) but has only been exercised against recorded responses, not the live API.
// If the schema moves, adjust query and response structs here: nothing else depends on them.
// Hardcover requires an API token (BIBLISANN_HARDCOVER_TOKEN) and asks clients to stay under
// 60 requests per minute; this package makes one request per refresh.
package hardcover

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	dbsqlc "github.com/larueli/biblisann/internal/db/sqlc"
	"github.com/larueli/biblisann/internal/safehttp"
)

const (
	endpoint = "https://api.hardcover.app/v1/graphql"
	source   = "hardcover"
	query    = `query($isbn: String!) { editions(where: {isbn_13: {_eq: $isbn}}, limit: 1) { book { slug rating ratings_count } } }`
)

// RateLimitedError is returned on HTTP 429; After says how long Hardcover asked us to wait.
type RateLimitedError struct{ After time.Duration }

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("hardcover: rate limited, retry in %s", e.After)
}

// ErrNoData means Hardcover does not know a rating for the book.
var ErrNoData = errors.New("no rating available")

// Doer sends requests through the SSRF-safe client.
type Doer interface {
	Do(req *http.Request) (*safehttp.Response, error)
}

// Service refreshes external grades.
type Service struct {
	q     *dbsqlc.Queries
	http  Doer
	token string
	url   string

	// Settings of the bulk refresh.
	Interval    time.Duration // minimum time between two requests (rate limit)
	RefreshDays int           // recheck a rated book after this many days
	NoneDays    int           // recheck an unrated book after this many days
	sleep       func(context.Context, time.Duration) error
}

// New creates the service; token is the Hardcover API token.
func New(pool *pgxpool.Pool, d Doer, token string) *Service {
	return &Service{q: dbsqlc.New(pool), http: d, token: strings.TrimPrefix(strings.TrimSpace(token), "Bearer "), url: endpoint,
		Interval: 1200 * time.Millisecond, RefreshDays: 3, NoneDays: 7, sleep: sleepCtx}
}

type response struct {
	Data struct {
		Editions []struct {
			Book struct {
				Slug         string   `json:"slug"`
				Rating       *float64 `json:"rating"`
				RatingsCount *int32   `json:"ratings_count"`
			} `json:"book"`
		} `json:"editions"`
	} `json:"data"`
	Errors []struct{ Message string } `json:"errors"`
}

// Fetch queries Hardcover for an ISBN-13 and returns (rating out of 5, votes, page URL).
func (s *Service) Fetch(ctx context.Context, isbn13 string) (float64, int32, string, error) {
	body, _ := json.Marshal(map[string]any{"query": query, "variables": map[string]string{"isbn": isbn13}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return 0, 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.token)
	resp, err := s.http.Do(req)
	if err != nil {
		return 0, 0, "", fmt.Errorf("hardcover: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, 0, "", err
	}
	if resp.Status == http.StatusTooManyRequests {
		after := time.Minute
		if n, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && n > 0 && n <= 3600 {
			after = time.Duration(n) * time.Second
		}
		return 0, 0, "", &RateLimitedError{After: after}
	}
	if resp.Status != 200 {
		return 0, 0, "", fmt.Errorf("hardcover: status %d", resp.Status)
	}
	var r response
	if err := json.Unmarshal(raw, &r); err != nil || len(r.Errors) > 0 {
		return 0, 0, "", errors.New("hardcover: unexpected response")
	}
	if len(r.Data.Editions) == 0 || r.Data.Editions[0].Book.Rating == nil {
		return 0, 0, "", ErrNoData
	}
	b := r.Data.Editions[0].Book
	var votes int32
	if b.RatingsCount != nil {
		votes = *b.RatingsCount
	}
	return *b.Rating, votes, "https://hardcover.app/books/" + b.Slug, nil
}

// Refresh fetches and stores the Hardcover rating of one work now (scale 5).
func (s *Service) Refresh(ctx context.Context, workID uuid.UUID) error {
	e, err := s.q.GetAnyISBNOfWork(ctx, workID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && e == nil) {
		return ErrNoData
	}
	if err != nil {
		return err
	}
	rating, votes, link, ferr := s.Fetch(ctx, *e)
	if serr := s.store(ctx, workID, rating, votes, link, ferr); serr != nil {
		return serr
	}
	return ferr
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// store writes the outcome of one lookup: the rating (or its absence) and the check record.
func (s *Service) store(ctx context.Context, workID uuid.UUID, rating float64, votes int32, link string, ferr error) error {
	switch {
	case ferr == nil:
		if err := s.q.UpsertExternalGrade(ctx, dbsqlc.UpsertExternalGradeParams{WorkID: workID, Source: source, Grade: float32(rating),
			Scale: 5, Votes: &votes, Url: link}); err != nil {
			return err
		}
		return s.q.RecordExternalGradeCheck(ctx, dbsqlc.RecordExternalGradeCheckParams{WorkID: workID, Source: source, Status: "ok"})
	case errors.Is(ferr, ErrNoData):
		_ = s.q.DeleteExternalGrade(ctx, dbsqlc.DeleteExternalGradeParams{WorkID: workID, Source: source}) // it may have been withdrawn
		return s.q.RecordExternalGradeCheck(ctx, dbsqlc.RecordExternalGradeCheckParams{WorkID: workID, Source: source, Status: "none"})
	}
	return s.q.RecordExternalGradeCheck(ctx, dbsqlc.RecordExternalGradeCheckParams{WorkID: workID, Source: source, Status: "error"})
}

// Sync looks up to budget due works up, one request at a time, never faster than Interval. It stops
// early when Hardcover asks to slow down (retryAfter > 0) or after three failures in a row; failed
// works are retried later with a growing delay. It returns how many works were processed.
func (s *Service) Sync(ctx context.Context, budget int) (processed int, retryAfter time.Duration, err error) {
	rows, err := s.q.ExternalGradeCandidates(ctx, dbsqlc.ExternalGradeCandidatesParams{Source: source,
		Column2: int32(s.RefreshDays), Column3: int32(s.NoneDays), Limit: int32(budget)})
	if err != nil {
		return 0, 0, err
	}
	consecutive := 0
	for i, r := range rows {
		if i > 0 {
			if err := s.sleep(ctx, s.Interval); err != nil {
				return processed, 0, err
			}
		}
		if r.Isbn13 == "" {
			continue
		}
		rating, votes, link, ferr := s.Fetch(ctx, r.Isbn13)
		var rl *RateLimitedError
		if errors.As(ferr, &rl) {
			return processed, rl.After, nil // do not record: the book was not really checked
		}
		if serr := s.store(ctx, r.WorkID, rating, votes, link, ferr); serr != nil {
			return processed, 0, serr
		}
		processed++
		if ferr != nil && !errors.Is(ferr, ErrNoData) {
			if consecutive++; consecutive >= 3 {
				return processed, 0, fmt.Errorf("hardcover: three failures in a row: %w", ferr)
			}
		} else {
			consecutive = 0
		}
	}
	return processed, 0, nil
}
