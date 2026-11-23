// Package providers defines the metadata provider interface and the built-in providers (BnF,
// Open Library). Everything a provider returns is untrusted text.
package providers

import (
	"context"
	"errors"
	"io"

	"github.com/larueli/biblisann/internal/safehttp"
)

// Record is the metadata one provider knows about one ISBN. Empty fields are unknown.
type Record struct {
	Title       string   `json:"title"`
	Subtitle    string   `json:"subtitle,omitempty"`
	Authors     []string `json:"authors,omitempty"`
	Publisher   string   `json:"publisher,omitempty"`
	Year        int      `json:"year,omitempty"`
	Language    string   `json:"language,omitempty"` // ISO 639-1 when known
	Pages       int      `json:"pages,omitempty"`
	Description string   `json:"description,omitempty"`
	Series      string   `json:"series,omitempty"`
	SeriesPos   *float64 `json:"series_position,omitempty"`
	CoverURL    string   `json:"cover_url,omitempty"`
}

// Provider looks a book up by ISBN-13.
type Provider interface {
	// Name is the stable identifier used in settings and provenance.
	Name() string
	// Lookup returns (nil, nil) when the provider does not know the book.
	Lookup(ctx context.Context, isbn13 string) (*Record, error)
}

// Fetcher is what providers need from the network layer. *safehttp.Client satisfies it.
type Fetcher interface {
	Get(ctx context.Context, url string) (*safehttp.Response, error)
}

// ErrUpstream wraps provider transport and protocol failures.
var ErrUpstream = errors.New("provider request failed")

// maxProviderBody bounds the body a provider response may have.
const maxProviderBody = 2 << 20

func readBody(r *safehttp.Response) ([]byte, error) {
	defer func() { _ = r.Body.Close() }()
	return io.ReadAll(io.LimitReader(r.Body, maxProviderBody))
}
