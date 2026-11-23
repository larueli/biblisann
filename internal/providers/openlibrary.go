package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// OpenLibrary queries the Open Library books API.
type OpenLibrary struct {
	Fetcher Fetcher
	BaseURL string // default https://openlibrary.org
}

// Name implements Provider.
func (OpenLibrary) Name() string { return "openlibrary" }

type olBook struct {
	Title       string                         `json:"title"`
	Subtitle    string                         `json:"subtitle"`
	Authors     []struct{ Name string }        `json:"authors"`
	Publishers  []struct{ Name string }        `json:"publishers"`
	PublishDate string                         `json:"publish_date"`
	Pages       int                            `json:"number_of_pages"`
	Cover       struct{ Large, Medium string } `json:"cover"`
}

var yearRe = regexp.MustCompile(`(1[5-9]|20)\d{2}`)

// Lookup implements Provider.
func (o OpenLibrary) Lookup(ctx context.Context, isbn13 string) (*Record, error) {
	base := o.BaseURL
	if base == "" {
		base = "https://openlibrary.org"
	}
	key := "ISBN:" + isbn13
	u := base + "/api/books?format=json&jscmd=data&bibkeys=" + url.QueryEscape(key)
	resp, err := o.Fetcher.Get(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("%w: openlibrary: %w", ErrUpstream, err)
	}
	if resp.Status != 200 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: openlibrary: status %d", ErrUpstream, resp.Status)
	}
	body, err := readBody(resp)
	if err != nil {
		return nil, fmt.Errorf("%w: openlibrary: %w", ErrUpstream, err)
	}
	var data map[string]olBook
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("%w: openlibrary: bad JSON", ErrUpstream)
	}
	b, ok := data[key]
	if !ok || strings.TrimSpace(b.Title) == "" {
		return nil, nil
	}
	r := &Record{Title: clean(b.Title), Subtitle: clean(b.Subtitle), Pages: b.Pages}
	for _, a := range b.Authors {
		if n := clean(a.Name); n != "" {
			r.Authors = append(r.Authors, n)
		}
	}
	if len(b.Publishers) > 0 {
		r.Publisher = clean(b.Publishers[0].Name)
	}
	if y := yearRe.FindString(b.PublishDate); y != "" {
		r.Year, _ = strconv.Atoi(y)
	}
	// Covers are fetched by ISBN; the URL in the response is just as good but less predictable.
	r.CoverURL = base2cover(isbn13)
	return r, nil
}

func base2cover(isbn13 string) string {
	return "https://covers.openlibrary.org/b/isbn/" + isbn13 + "-L.jpg?default=false"
}

// clean trims and collapses whitespace and drops control characters.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}
