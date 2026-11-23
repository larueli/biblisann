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
	"strings"

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
}

// New creates the service; token is the Hardcover API token.
func New(pool *pgxpool.Pool, d Doer, token string) *Service {
	return &Service{q: dbsqlc.New(pool), http: d, token: strings.TrimPrefix(strings.TrimSpace(token), "Bearer "), url: endpoint}
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

// Refresh fetches and stores the Hardcover rating of a work (scale 5).
func (s *Service) Refresh(ctx context.Context, workID uuid.UUID) error {
	e, err := s.q.GetAnyISBNOfWork(ctx, workID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && e == nil) {
		return ErrNoData
	}
	if err != nil {
		return err
	}
	rating, votes, link, err := s.Fetch(ctx, *e)
	if err != nil {
		return err
	}
	return s.q.UpsertExternalGrade(ctx, dbsqlc.UpsertExternalGradeParams{WorkID: workID, Source: source, Grade: float32(rating),
		Scale: 5, Votes: &votes, Url: link})
}
